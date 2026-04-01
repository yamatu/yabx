package node

import (
	"fmt"
	"sort"

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
			if c.dynamicSpeedLimitEnabled() {
				c.addTraffic(c.userList[i].Uuid, up+down)
			}
			userTraffic = append(userTraffic, panel.UserTraffic{
				UID:      (c.userList)[i].Id,
				Upload:   up,
				Download: down})
			reportedUID[(c.userList)[i].Id] = struct{}{}
		}
	}

	onlineDevice, err := c.getOnlineUsers()
	if err != nil {
		log.WithFields(log.Fields{
			"tag": c.tag,
			"err": err,
		}).Info("Get online users failed")
	} else {
		onlineDevice = dedupeOnlineUsersByIP(onlineDevice)
		// Only report users whose period traffic reaches DeviceOnlineMinTraffic.
		// Keep traffic filter behavior for ppanel only.
		// XBoard/UniProxy expects real-time online device reporting even with tiny traffic.
		result := make([]panel.OnlineUser, 0, len(onlineDevice))
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
		for _, online := range onlineDevice {
			if _, ok := nocountUID[online.UID]; !ok {
				result = append(result, online)
			}
		}
		data := buildOnlineIPPayload(result, c.userList)

		// XBoard node online count is based on /push payload count.
		// Include zero-traffic online users for non-ppanel to keep node online count aligned with online users.
		if c.apiClient.PanelType != "ppanel" {
			for _, onlineuser := range result {
				uid := onlineuser.UID
				if _, ok := reportedUID[uid]; !ok {
					userTraffic = append(userTraffic, panel.UserTraffic{UID: uid, Upload: 0, Download: 0})
					reportedUID[uid] = struct{}{}
				}
			}
		}

		if err = c.apiClient.ReportNodeOnlineUsers(&data); err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Info("Report online users failed")
		} else {
			log.WithField("tag", c.tag).Infof("Total %d online users, %d Reported", len(onlineDevice), len(result))
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

func (c *Controller) syncOnlineUsersTask() error {
	if c.limiter == nil {
		return nil
	}

	data, err := c.getOnlineIPMap()
	if err != nil {
		log.WithFields(log.Fields{
			"tag": c.tag,
			"err": err,
		}).Warn("Build online IP sync payload failed")
		return nil
	}

	data = dedupeOnlineIPMapByIP(data)
	data = buildOnlineIPMapPayload(data, c.userList)

	if err = c.apiClient.ReportNodeOnlineUsers(&data); err != nil {
		log.WithFields(log.Fields{
			"tag": c.tag,
			"err": err,
		}).Warn("Sync online IP data failed")
	}

	aliveMap, err := c.apiClient.GetUserAlive()
	if err != nil {
		log.WithFields(log.Fields{
			"tag": c.tag,
			"err": err,
		}).Warn("Refresh synced alive list failed")
		return nil
	}

	c.aliveMap = aliveMap
	c.limiter.SetAliveList(aliveMap)
	return nil
}

func dedupeOnlineUsersByIP(users []panel.OnlineUser) []panel.OnlineUser {
	if len(users) <= 1 {
		return users
	}

	seen := make(map[string]struct{}, len(users))
	result := make([]panel.OnlineUser, 0, len(users))
	for _, onlineUser := range users {
		if onlineUser.IP == "" {
			continue
		}
		if _, ok := seen[onlineUser.IP]; ok {
			continue
		}
		seen[onlineUser.IP] = struct{}{}
		result = append(result, onlineUser)
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].IP != result[j].IP {
			return result[i].IP < result[j].IP
		}
		return result[i].UID < result[j].UID
	})
	return result
}

func dedupeOnlineIPMapByIP(data map[int][]string) map[int][]string {
	if len(data) <= 1 {
		return data
	}

	uids := make([]int, 0, len(data))
	for uid := range data {
		uids = append(uids, uid)
	}
	sort.Ints(uids)

	seen := make(map[string]struct{})
	result := make(map[int][]string, len(data))
	for _, uid := range uids {
		for _, ip := range data[uid] {
			if ip == "" {
				continue
			}
			if _, ok := seen[ip]; ok {
				continue
			}
			seen[ip] = struct{}{}
			result[uid] = append(result[uid], ip)
		}
	}
	return result
}

func userCompareKey(user panel.UserInfo) string {
	return fmt.Sprintf("%d|%s|%d|%d", user.Id, user.Uuid, user.SpeedLimit, user.DeviceLimit)
}

func compareUserList(old, new []panel.UserInfo) (deleted, added []panel.UserInfo) {
	oldMap := make(map[string]int)
	for i, user := range old {
		key := userCompareKey(user)
		oldMap[key] = i
	}

	for _, user := range new {
		key := userCompareKey(user)
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

func buildOnlineIPPayload(onlineUsers []panel.OnlineUser, users []panel.UserInfo) map[int][]string {
	data := make(map[int][]string, len(users))
	for _, onlineuser := range onlineUsers {
		data[onlineuser.UID] = append(data[onlineuser.UID], onlineuser.IP)
	}
	return buildOnlineIPMapPayload(data, users)
}

func buildOnlineIPMapPayload(data map[int][]string, users []panel.UserInfo) map[int][]string {
	if data == nil {
		data = make(map[int][]string, len(users))
	}
	for _, user := range users {
		if _, ok := data[user.Id]; !ok {
			data[user.Id] = []string{}
		}
	}
	return data
}
