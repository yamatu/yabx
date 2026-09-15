package node

import (
	"time"

	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/common/task"
	vCore "github.com/InazumaV/V2bX/core"
	"github.com/InazumaV/V2bX/limiter"
	log "github.com/sirupsen/logrus"
)

func (c *Controller) normalizedPushInterval(interval time.Duration) time.Duration {
	if interval <= 0 {
		interval = 60 * time.Second
	}

	if c.apiClient.PanelType != "ppanel" {
		if interval < 20*time.Second {
			interval = 20 * time.Second
		}
		if interval > 4*time.Minute {
			interval = 4 * time.Minute
		}
	}

	return interval
}

// applyOnlineTTL keeps the online-device window strictly longer than the panel
// push interval, otherwise a device could be dropped between two reports and
// make the reported online count flap.
func (c *Controller) applyOnlineTTL(pushInterval time.Duration) {
	if c.limiter == nil {
		return
	}
	ttl := time.Duration(c.LimitConfig.OnlineTimeout) * time.Second
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	if minimum := pushInterval + time.Minute; ttl < minimum {
		log.WithField("tag", c.tag).Warnf(
			"OnlineTimeout %s is not greater than the push interval %s, raising it to %s",
			ttl, pushInterval, minimum)
		ttl = minimum
	}
	c.limiter.SetOnlineTTL(ttl)
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

	for _, user := range c.userList {
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
	log.WithField("tag", c.tag).Info("Start online IP sync")
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
	log.WithField("tag", c.tag).Info("Start monitor node status")
	// delay to start nodeInfoMonitor
	_ = c.nodeInfoMonitorPeriodic.Start(false)
	log.WithField("tag", c.tag).Info("Start report node status")
	_ = c.userReportPeriodic.Start(true)
	if node.Security == panel.Tls {
		switch c.CertConfig.CertMode {
		case "none", "", "file", "self":
		default:
			c.renewCertPeriodic = &task.Task{
				Interval: time.Hour * 24,
				Execute:  c.renewCertTask,
			}
			log.WithField("tag", c.tag).Info("Start renew cert")
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
			"tag": c.tag,
			"err": err,
		}).Error("Get node info failed")
		return nil
	}
	// get user info
	newU, err := c.apiClient.GetUserList()
	if err != nil {
		log.WithFields(log.Fields{
			"tag": c.tag,
			"err": err,
		}).Error("Get user list failed")
		return nil
	}
	// get user alive
	newA, err := c.apiClient.GetUserAlive()
	if err != nil {
		log.WithFields(log.Fields{
			"tag": c.tag,
			"err": err,
		}).Error("Get alive list failed")
		return nil
	}
	if newN != nil {
		c.info = newN
		// nodeInfo changed
		if newU != nil {
			c.userList = newU
		}
		c.resetTraffic()
		// Remove old node
		log.WithField("tag", c.tag).Info("Node changed, reload")
		err = c.server.DelNode(c.tag)
		if err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Panic("Delete node failed")
			return nil
		}

		// Update limiter
		if len(c.Options.Name) == 0 {
			c.tag = c.buildNodeTag(newN)
			// Remove Old limiter
			limiter.DeleteLimiter(c.tag)
			// Add new Limiter
			l := limiter.AddLimiter(c.tag, &c.LimitConfig, c.userList, newA)
			c.limiter = l
		}
		// update alive list
		if newA != nil {
			c.limiter.SetAliveList(newA)
		}
		// Update rule
		err = c.limiter.UpdateRule(&newN.Rules)
		if err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Error("Update Rule failed")
			return nil
		}

		// check cert
		if newN.Security == panel.Tls {
			err = c.requestCert()
			if err != nil {
				log.WithFields(log.Fields{
					"tag": c.tag,
					"err": err,
				}).Error("Request cert failed")
				return nil
			}
		}
		// add new node
		err = c.server.AddNode(c.tag, newN, c.Options)
		if err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Panic("Add node failed")
			return nil
		}
		_, err = c.server.AddUsers(&vCore.AddUsersParams{
			Tag:      c.tag,
			Users:    c.userList,
			NodeInfo: newN,
		})
		if err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Error("Add users failed")
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
		log.WithField("tag", c.tag).Infof("Added %d new users", len(c.userList))
		// exit
		return nil
	}
	// update alive list
	if newA != nil {
		c.limiter.SetAliveList(newA)
	}
	// node no changed, check users
	if len(newU) == 0 {
		return nil
	}
	deleted, added := compareUserList(c.userList, newU)
	if len(deleted) > 0 {
		// have deleted users
		err = c.server.DelUsers(deleted, c.tag, c.info)
		if err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Error("Delete users failed")
			return nil
		}
	}
	if len(added) > 0 {
		// have added users
		_, err = c.server.AddUsers(&vCore.AddUsersParams{
			Tag:      c.tag,
			NodeInfo: c.info,
			Users:    added,
		})
		if err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Error("Add users failed")
			return nil
		}
	}
	if len(added) > 0 || len(deleted) > 0 {
		// update Limiter
		c.limiter.UpdateUser(c.tag, added, deleted)
		if err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
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
	c.userList = newU
	c.ensureOnlineIPSyncTask()
	if len(added)+len(deleted) != 0 {
		log.WithField("tag", c.tag).
			Infof("%d user deleted, %d user added", len(deleted), len(added))
	}
	return nil
}

func (c *Controller) SpeedChecker() error {
	if !c.dynamicSpeedLimitEnabled() {
		return nil
	}

	for _, uuid := range c.consumeDynamicSpeedLimitUsers() {
		err := c.limiter.UpdateDynamicSpeedLimit(
			c.tag,
			uuid,
			c.LimitConfig.DynamicSpeedLimitConfig.SpeedLimit,
			time.Now().Add(time.Duration(c.LimitConfig.DynamicSpeedLimitConfig.ExpireTime)*time.Minute),
		)
		if err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Error("Update dynamic speed limit failed")
		}
	}
	return nil
}
