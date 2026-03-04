package node

import (
	"strconv"

	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/common/serverstatus"
	log "github.com/sirupsen/logrus"
)

func (c *Controller) reportUserTrafficTask() (err error) {
	// Get User traffic
	userTraffic := make([]panel.UserTraffic, 0)
	reportedUID := make(map[int]struct{})
	for i := range c.userList {
		up, down := c.server.GetUserTraffic(c.tag, c.userList[i].Uuid, true)
		if up > 0 || down > 0 {
			if c.LimitConfig.EnableDynamicSpeedLimit {
				c.traffic[c.userList[i].Uuid] += up + down
			}
			userTraffic = append(userTraffic, panel.UserTraffic{
				UID:      (c.userList)[i].Id,
				Upload:   up,
				Download: down})
			reportedUID[(c.userList)[i].Id] = struct{}{}
		}
	}

	onlineDevice, err := c.limiter.GetOnlineDevice()
	if err != nil {
		log.WithFields(log.Fields{
			"tag": c.tag,
			"err": err,
		}).Info("Get online users failed")
	} else {
		// Only report users whose period traffic reaches DeviceOnlineMinTraffic.
		// Keep traffic filter behavior for ppanel only.
		// XBoard/UniProxy expects real-time online device reporting even with tiny traffic.
		result := make([]panel.OnlineUser, 0, len(*onlineDevice))
		nocountUID := make(map[int]struct{})
		applyTrafficFilter := c.apiClient.PanelType == "ppanel" && c.Options.DeviceOnlineMinTraffic > 0
		if applyTrafficFilter {
			for _, traffic := range userTraffic {
				total := traffic.Upload + traffic.Download
				if total < int64(c.Options.DeviceOnlineMinTraffic*1000) {
					nocountUID[traffic.UID] = struct{}{}
				}
			}
		}
		for _, online := range *onlineDevice {
			if _, ok := nocountUID[online.UID]; !ok {
				result = append(result, online)
			}
		}
		data := make(map[int][]string)
		for _, onlineuser := range result {
			// json structure: { UID1:["ip1","ip2"],UID2:["ip3","ip4"] }
			data[onlineuser.UID] = append(data[onlineuser.UID], onlineuser.IP)
		}

		// XBoard node online count is based on /push payload count.
		// Include zero-traffic online users for non-ppanel to keep node online count accurate.
		if c.apiClient.PanelType != "ppanel" {
			for _, onlineuser := range result {
				if _, ok := reportedUID[onlineuser.UID]; !ok {
					userTraffic = append(userTraffic, panel.UserTraffic{UID: onlineuser.UID, Upload: 0, Download: 0})
					reportedUID[onlineuser.UID] = struct{}{}
				}
			}
		}

		if err = c.apiClient.ReportNodeOnlineUsers(&data); err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Info("Report online users failed")
		} else {
			log.WithField("tag", c.tag).Infof("Total %d online users, %d Reported", len(*onlineDevice), len(result))
		}
	}

	if len(userTraffic) > 0 {
		err = c.apiClient.ReportUserTraffic(userTraffic)
		if err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Info("Report user traffic failed")
		} else {
			log.WithField("tag", c.tag).Infof("Report %d users traffic", len(userTraffic))
		}
	}

	status, statusErr := serverstatus.GetSystemStatus()
	if statusErr != nil {
		log.WithFields(log.Fields{
			"tag": c.tag,
			"err": statusErr,
		}).Warn("Get system status failed")
	}

	nodeStatus := &panel.NodeStatus{}
	if status != nil {
		nodeStatus.CPU = status.CPU
		nodeStatus.Uptime = status.Uptime
		nodeStatus.MemTotal = status.MemTotal
		nodeStatus.MemUsed = status.MemUsed
		nodeStatus.SwapTotal = status.SwapTotal
		nodeStatus.SwapUsed = status.SwapUsed
		nodeStatus.DiskTotal = status.DiskTotal
		nodeStatus.DiskUsed = status.DiskUsed
		if status.MemTotal > 0 {
			nodeStatus.Mem = float64(status.MemUsed) / float64(status.MemTotal) * 100
		}
		if status.DiskTotal > 0 {
			nodeStatus.Disk = float64(status.DiskUsed) / float64(status.DiskTotal) * 100
		}
	}

	err = c.apiClient.ReportNodeStatus(nodeStatus)
	if err != nil {
		log.WithFields(log.Fields{
			"tag": c.tag,
			"err": err,
		}).Info("Report node status failed")
	}

	userTraffic = nil
	return nil
}

func compareUserList(old, new []panel.UserInfo) (deleted, added []panel.UserInfo) {
	oldMap := make(map[string]int)
	for i, user := range old {
		key := user.Uuid + strconv.Itoa(user.SpeedLimit)
		oldMap[key] = i
	}

	for _, user := range new {
		key := user.Uuid + strconv.Itoa(user.SpeedLimit)
		if _, exists := oldMap[key]; !exists {
			added = append(added, user)
		} else {
			delete(oldMap, key)
		}
	}

	for _, index := range oldMap {
		deleted = append(deleted, old[index])
	}

	return deleted, added
}
