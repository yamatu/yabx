package node

import (
	"errors"
	"fmt"
	"sync"

	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/common/task"
	"github.com/InazumaV/V2bX/conf"
	vCore "github.com/InazumaV/V2bX/core"
	"github.com/InazumaV/V2bX/limiter"
	log "github.com/sirupsen/logrus"
)

type Controller struct {
	server                    vCore.Core
	apiClient                 *panel.Client
	trafficMu                 sync.Mutex
	traffic                   map[string]int64
	onlineMu                  sync.Mutex
	lastOnlineUIDs            map[int]struct{}
	nodeInfoMonitorPeriodic   *task.Task
	userReportPeriodic        *task.Task
	renewCertPeriodic         *task.Task
	dynamicSpeedLimitPeriodic *task.Task
	onlineIpReportPeriodic    *task.Task
	*conf.Options

	// stateMu guards the fields below. nodeInfoMonitor rewrites them (a reload
	// changes the tag, the limiter and the user list) while the reporting tasks
	// read them from their own goroutine. Read them through the getters, write
	// them through the setters, never directly.
	stateMu  sync.RWMutex
	tag      string
	limiter  *limiter.Limiter
	userList []panel.UserInfo
	info     *panel.NodeInfo
}

// The state getters return the stored value for a short read. The user list is
// shared, not copied: it is only ever replaced as a whole, never modified in
// place, so a reader that does not write to it is safe.

func (c *Controller) getTag() string {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()

	return c.tag
}

func (c *Controller) setTag(tag string) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()

	c.tag = tag
}

func (c *Controller) getLimiter() *limiter.Limiter {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()

	return c.limiter
}

func (c *Controller) setLimiter(l *limiter.Limiter) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()

	c.limiter = l
}

func (c *Controller) getUsers() []panel.UserInfo {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()

	return c.userList
}

func (c *Controller) setUsers(users []panel.UserInfo) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()

	c.userList = users
}

func (c *Controller) getInfo() *panel.NodeInfo {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()

	return c.info
}

func (c *Controller) setInfo(info *panel.NodeInfo) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()

	c.info = info
}

// NewController return a Node controller with default parameters.
func NewController(server vCore.Core, api *panel.Client, config *conf.Options) *Controller {
	controller := &Controller{
		server:         server,
		Options:        config,
		apiClient:      api,
		lastOnlineUIDs: make(map[int]struct{}),
	}
	return controller
}

// Start implement the Start() function of the service interface
func (c *Controller) Start() error {
	// First fetch Node Info
	var err error
	node, err := c.apiClient.GetNodeInfo()
	if err != nil {
		return fmt.Errorf("get node info error: %s", err)
	}
	// Update user
	userList, err := c.apiClient.GetUserList()
	if err != nil {
		return fmt.Errorf("get user list error: %s", err)
	}
	if len(userList) == 0 {
		return errors.New("add users error: not have any user")
	}
	aliveMap, err := c.apiClient.GetUserAlive()
	if err != nil {
		return fmt.Errorf("failed to get user alive list: %s", err)
	}
	tag := c.Options.Name
	if len(tag) == 0 {
		tag = c.buildNodeTag(node)
	}

	// add limiter
	l := limiter.AddLimiter(tag, &c.LimitConfig, userList, aliveMap)
	// add rule limiter
	if err = l.UpdateRule(&node.Rules); err != nil {
		return fmt.Errorf("update rule error: %s", err)
	}
	c.setTag(tag)
	c.setUsers(userList)
	c.setLimiter(l)
	if node.Security == panel.Tls {
		err = c.requestCert()
		if err != nil {
			return fmt.Errorf("request cert error: %s", err)
		}
	}
	// Add new tag
	err = c.server.AddNode(tag, node, c.Options)
	if err != nil {
		return fmt.Errorf("add new node error: %s", err)
	}
	added, err := c.server.AddUsers(&vCore.AddUsersParams{
		Tag:      tag,
		Users:    userList,
		NodeInfo: node,
	})
	if err != nil {
		return fmt.Errorf("add users error: %s", err)
	}
	log.WithField("tag", tag).Infof("Added %d new users", added)
	c.setInfo(node)
	c.startTasks(node)
	return nil
}

// Close implement the Close() function of the service interface
func (c *Controller) Close() error {
	tag := c.getTag()
	limiter.DeleteLimiter(tag)
	if c.nodeInfoMonitorPeriodic != nil {
		c.nodeInfoMonitorPeriodic.Close()
	}
	if c.userReportPeriodic != nil {
		c.userReportPeriodic.Close()
	}
	if c.renewCertPeriodic != nil {
		c.renewCertPeriodic.Close()
	}
	if c.dynamicSpeedLimitPeriodic != nil {
		c.dynamicSpeedLimitPeriodic.Close()
	}
	if c.onlineIpReportPeriodic != nil {
		c.onlineIpReportPeriodic.Close()
	}
	err := c.server.DelNode(tag)
	if err != nil {
		return fmt.Errorf("del node error: %s", err)
	}
	return nil
}

func (c *Controller) buildNodeTag(node *panel.NodeInfo) string {
	return fmt.Sprintf("[%s]-%s:%d", c.apiClient.APIHost, node.Type, node.Id)
}

func (c *Controller) resetTraffic() {
	c.trafficMu.Lock()
	defer c.trafficMu.Unlock()

	c.traffic = make(map[string]int64)
}

func (c *Controller) addTraffic(uuid string, traffic int64) {
	if traffic <= 0 {
		return
	}

	c.trafficMu.Lock()
	defer c.trafficMu.Unlock()

	if c.traffic == nil {
		c.traffic = make(map[string]int64)
	}
	c.traffic[uuid] += traffic
}

func (c *Controller) deleteTraffic(uuid string) {
	c.trafficMu.Lock()
	defer c.trafficMu.Unlock()

	delete(c.traffic, uuid)
}

func (c *Controller) consumeDynamicSpeedLimitUsers() []string {
	if !c.dynamicSpeedLimitEnabled() {
		return nil
	}

	threshold := c.LimitConfig.DynamicSpeedLimitConfig.Traffic
	if threshold <= 0 {
		return nil
	}

	c.trafficMu.Lock()
	defer c.trafficMu.Unlock()

	matched := make([]string, 0)
	for uuid, traffic := range c.traffic {
		if traffic >= threshold {
			matched = append(matched, uuid)
			delete(c.traffic, uuid)
		}
	}

	return matched
}
