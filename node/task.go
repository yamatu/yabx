package node

import (
	"time"

	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/common/task"
	vCore "github.com/InazumaV/V2bX/core"
	"github.com/InazumaV/V2bX/limiter"
	log "github.com/sirupsen/logrus"
)

// maxPanelPushInterval keeps the /push cycle inside the window the panel uses to
// decide whether a user is online. XBoard marks a user as 当前在线 when
// users.t (written by every /push) is younger than 120s, so a longer cycle made
// an otherwise connected user flap to 最后在线时间 between two reports.
const maxPanelPushInterval = 100 * time.Second

func (c *Controller) normalizedPushInterval(interval time.Duration) time.Duration {
	if interval <= 0 {
		interval = 60 * time.Second
	}

	if c.apiClient.PanelType != "ppanel" {
		if interval < 20*time.Second {
			interval = 20 * time.Second
		}
		if interval > maxPanelPushInterval {
			log.WithField("tag", c.getTag()).Warnf(
				"Panel push interval %s exceeds the online status window, lowering it to %s",
				interval, maxPanelPushInterval)
			interval = maxPanelPushInterval
		}
	}

	return interval
}

// retryNodeReload forgets the cached node config so the next pull returns it
// again. Without it a half applied reload is never retried: the panel answers
// 304 (ETag match) or the exact same body hash from then on, and the node would
// stay on the previous configuration until an admin changes something else.
func (c *Controller) retryNodeReload() {
	if c.apiClient != nil {
		c.apiClient.InvalidateNodeConfigCache()
	}
}

// applyOnlineTTL keeps the online-device window strictly longer than the panel
// push interval, otherwise a device could be dropped between two reports and
// make the reported online count flap.
func (c *Controller) applyOnlineTTL(pushInterval time.Duration) {
	l := c.getLimiter()
	if l == nil {
		return
	}
	ttl := time.Duration(c.LimitConfig.OnlineTimeout) * time.Second
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	if minimum := pushInterval + time.Minute; ttl < minimum {
		log.WithField("tag", c.getTag()).Warnf(
			"OnlineTimeout %s is not greater than the push interval %s, raising it to %s",
			ttl, pushInterval, minimum)
		ttl = minimum
	}
	l.SetOnlineTTL(ttl)
}

func (c *Controller) dynamicSpeedLimitEnabled() bool {
	return c.LimitConfig.EnableDynamicSpeedLimit && c.LimitConfig.DynamicSpeedLimitConfig != nil
}

func (c *Controller) dynamicSpeedLimitInterval() time.Duration {
	if c.dynamicSpeedLimitEnabled() && c.LimitConfig.DynamicSpeedLimitConfig.Periodic > 0 {
		return time.Duration(c.LimitConfig.DynamicSpeedLimitConfig.Periodic) * time.Second
	}

	return 60 * time.Second
}

func (c *Controller) onlineIPSyncEnabled() bool {
	if c.apiClient == nil || c.apiClient.PanelType == "ppanel" {
		return false
	}

	if c.LimitConfig.EnableIpRecorder &&
		c.LimitConfig.IpRecorderConfig != nil &&
		c.LimitConfig.IpRecorderConfig.EnableIpSync {
		return true
	}

	for _, user := range c.getUsers() {
		if user.DeviceLimit > 0 {
			return true
		}
	}

	return false
}

func (c *Controller) onlineIPSyncInterval() time.Duration {
	if c.LimitConfig.IpRecorderConfig != nil && c.LimitConfig.IpRecorderConfig.Periodic > 0 {
		return time.Duration(c.LimitConfig.IpRecorderConfig.Periodic) * time.Second
	}

	return 30 * time.Second
}

func (c *Controller) ensureOnlineIPSyncTask() {
	if !c.onlineIPSyncEnabled() {
		if c.onlineIpReportPeriodic != nil {
			c.onlineIpReportPeriodic.Close()
			c.onlineIpReportPeriodic = nil
		}
		return
	}

	interval := c.onlineIPSyncInterval()
	if c.onlineIpReportPeriodic != nil && c.onlineIpReportPeriodic.Interval == interval {
		return
	}

	if c.onlineIpReportPeriodic != nil {
		c.onlineIpReportPeriodic.Close()
	}

	c.onlineIpReportPeriodic = &task.Task{
		Interval: interval,
		Execute:  c.syncOnlineUsersTask,
	}
	log.WithField("tag", c.getTag()).Info("Start online IP sync")
	_ = c.onlineIpReportPeriodic.Start(true)
}

func (c *Controller) startTasks(node *panel.NodeInfo) {
	pushInterval := c.normalizedPushInterval(node.PushInterval)
	c.applyOnlineTTL(pushInterval)

	// fetch node info task
	c.nodeInfoMonitorPeriodic = &task.Task{
		Interval: node.PullInterval,
		Execute:  c.nodeInfoMonitor,
	}
	// fetch user list task
	c.userReportPeriodic = &task.Task{
		Interval: pushInterval,
		Execute:  c.reportUserTrafficTask,
	}
	log.WithField("tag", c.getTag()).Info("Start monitor node status")
	// delay to start nodeInfoMonitor
	_ = c.nodeInfoMonitorPeriodic.Start(false)
	log.WithField("tag", c.getTag()).Info("Start report node status")
	_ = c.userReportPeriodic.Start(true)
	if node.Security == panel.Tls {
		switch c.CertConfig.CertMode {
		case "none", "", "file", "self":
		default:
			c.renewCertPeriodic = &task.Task{
				Interval: time.Hour * 24,
				Execute:  c.renewCertTask,
			}
			log.WithField("tag", c.getTag()).Info("Start renew cert")
			// delay to start renewCert
			_ = c.renewCertPeriodic.Start(true)
		}
	}
	if c.dynamicSpeedLimitEnabled() {
		c.resetTraffic()
		c.dynamicSpeedLimitPeriodic = &task.Task{
			Interval: c.dynamicSpeedLimitInterval(),
			Execute:  c.SpeedChecker,
		}
		log.Printf("[%s: %d] Start dynamic speed limit", c.apiClient.NodeType, c.apiClient.NodeId)
		_ = c.dynamicSpeedLimitPeriodic.Start(false)
	}
	c.ensureOnlineIPSyncTask()
}

func (c *Controller) nodeInfoMonitor() (err error) {
	// get node info
	newN, err := c.apiClient.GetNodeInfo()
	if err != nil {
		log.WithFields(log.Fields{
			"tag": c.getTag(),
			"err": err,
		}).Error("Get node info failed")
		return nil
	}
	// get user info
	newU, err := c.apiClient.GetUserList()
	if err != nil {
		log.WithFields(log.Fields{
			"tag": c.getTag(),
			"err": err,
		}).Error("Get user list failed")
		return nil
	}
	// get user alive
	newA, err := c.apiClient.GetUserAlive()
	if err != nil {
		log.WithFields(log.Fields{
			"tag": c.getTag(),
			"err": err,
		}).Error("Get alive list failed")
		return nil
	}
	if newN != nil {
		c.setInfo(newN)
		// nodeInfo changed
		if newU != nil {
			c.setUsers(newU)
		}
		c.resetTraffic()
		// Remove old node
		log.WithField("tag", c.getTag()).Info("Node changed, reload")

		// A node that is already gone is not an error: the new one is registered
		// right below, and aborting here would drop the reload entirely.
		if err = c.server.DelNode(c.getTag()); err != nil {
			log.WithFields(log.Fields{
				"tag": c.getTag(),
				"err": err,
			}).Error("Delete node failed")
		}

		// Update limiter
		if len(c.Options.Name) == 0 {
			oldTag := c.getTag()
			newTag := c.buildNodeTag(newN)
			c.setTag(newTag)
			// Remove the limiter of the OLD tag. Deleting with the freshly built
			// tag leaked the previous limiter (and its online registry) on every
			// reload, and those leaks were swept once a minute forever.
			limiter.DeleteLimiter(oldTag)
			// Add new Limiter
			l := limiter.AddLimiter(newTag, &c.LimitConfig, c.getUsers(), newA)
			c.setLimiter(l)
		}
		// update alive list
		if newA != nil {
			c.getLimiter().SetAliveList(newA)
		}
		// Update rule
		if err = c.getLimiter().UpdateRule(&newN.Rules); err != nil {
			log.WithFields(log.Fields{
				"tag": c.getTag(),
				"err": err,
			}).Error("Update Rule failed")
			c.retryNodeReload()
			return nil
		}

		// check cert
		if newN.Security == panel.Tls {
			if err = c.requestCert(); err != nil {
				log.WithFields(log.Fields{
					"tag": c.getTag(),
					"err": err,
				}).Error("Request cert failed")
				c.retryNodeReload()
				return nil
			}
		}
		// add new node
		if err = c.server.AddNode(c.getTag(), newN, c.Options); err != nil {
			log.WithFields(log.Fields{
				"tag": c.getTag(),
				"err": err,
			}).Error("Add node failed, retrying on the next pull")
			c.retryNodeReload()
			return nil
		}
		if _, err = c.server.AddUsers(&vCore.AddUsersParams{
			Tag:      c.getTag(),
			Users:    c.getUsers(),
			NodeInfo: newN,
		}); err != nil {
			log.WithFields(log.Fields{
				"tag": c.getTag(),
				"err": err,
			}).Error("Add users failed")
			c.retryNodeReload()
			return nil
		}
		// Check interval
		if c.nodeInfoMonitorPeriodic.Interval != newN.PullInterval &&
			newN.PullInterval != 0 {
			c.nodeInfoMonitorPeriodic.Interval = newN.PullInterval
			c.nodeInfoMonitorPeriodic.Close()
			_ = c.nodeInfoMonitorPeriodic.Start(false)
		}
		newPushInterval := c.normalizedPushInterval(newN.PushInterval)
		if c.userReportPeriodic.Interval != newPushInterval {
			c.userReportPeriodic.Interval = newPushInterval
			c.userReportPeriodic.Close()
			_ = c.userReportPeriodic.Start(true)
		}
		c.applyOnlineTTL(newPushInterval)
		c.ensureOnlineIPSyncTask()
		log.WithField("tag", c.getTag()).Infof("Added %d new users", len(c.getUsers()))
		// exit
		return nil
	}
	// update alive list
	if newA != nil {
		c.getLimiter().SetAliveList(newA)
	}
	// node no changed, check users.
	// GetUserList returns nil for "not modified" (304), so an empty but non nil
	// list means the node lost every user and the core must drop them as well.
	if newU == nil {
		return nil
	}
	deleted, added := compareUserList(c.getUsers(), newU)
	if len(deleted) > 0 {
		// have deleted users
		err = c.server.DelUsers(deleted, c.getTag(), c.getInfo())
		if err != nil {
			log.WithFields(log.Fields{
				"tag": c.getTag(),
				"err": err,
			}).Error("Delete users failed")
			return nil
		}
	}
	if len(added) > 0 {
		// have added users
		_, err = c.server.AddUsers(&vCore.AddUsersParams{
			Tag:      c.getTag(),
			NodeInfo: c.getInfo(),
			Users:    added,
		})
		if err != nil {
			log.WithFields(log.Fields{
				"tag": c.getTag(),
				"err": err,
			}).Error("Add users failed")
			return nil
		}
	}
	if len(added) > 0 || len(deleted) > 0 {
		// update Limiter
		c.getLimiter().UpdateUser(c.getTag(), added, deleted)
		if err != nil {
			log.WithFields(log.Fields{
				"tag": c.getTag(),
				"err": err,
			}).Error("limiter users failed")
			return nil
		}
		// clear traffic record
		if c.dynamicSpeedLimitEnabled() {
			for i := range deleted {
				c.deleteTraffic(deleted[i].Uuid)
			}
		}
	}
	c.setUsers(newU)
	c.ensureOnlineIPSyncTask()
	if len(added)+len(deleted) != 0 {
		log.WithField("tag", c.getTag()).
			Infof("%d user deleted, %d user added", len(deleted), len(added))
	}
	return nil
}

func (c *Controller) SpeedChecker() error {
	if !c.dynamicSpeedLimitEnabled() {
		return nil
	}

	for _, uuid := range c.consumeDynamicSpeedLimitUsers() {
		err := c.getLimiter().UpdateDynamicSpeedLimit(
			c.getTag(),
			uuid,
			c.LimitConfig.DynamicSpeedLimitConfig.SpeedLimit,
			time.Now().Add(time.Duration(c.LimitConfig.DynamicSpeedLimitConfig.ExpireTime)*time.Minute),
		)
		if err != nil {
			log.WithFields(log.Fields{
				"tag": c.getTag(),
				"err": err,
			}).Error("Update dynamic speed limit failed")
		}
	}
	return nil
}
