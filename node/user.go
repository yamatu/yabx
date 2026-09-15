package node

import (
	"fmt"
	"sort"

	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/common/netutil"
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

// syncOnlineUsersTask refreshes the panel side device count (alivelist) used to
// enforce per-user device limits.
//
// Online devices are reported from reportUserTrafficTask so that the /alive
// payload and the /push online count always come from the same snapshot. Having
// two tasks post /alive with different sources made XBoard's setDevices wipe and
// rewrite the device set on every run, which produced a fluctuating count.
func (c *Controller) syncOnlineUsersTask() error {
	if c.limiter == nil {
		return nil
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

// dedupeOnlineUsersByIP removes duplicate (uid, ip) pairs.
//
// Deduplication MUST be scoped per user. XBoard counts devices per user
// (user_devices:{uid}) and already deduplicates by IP inside a user, so a global
// IP based dedupe drops every other user that shares an exit IP (NAT, same
// router, office network) and under-reports the online count.
func dedupeOnlineUsersByIP(users []panel.OnlineUser) []panel.OnlineUser {
	type onlineKey struct {
		uid int
		ip  string
	}
	seen := make(map[onlineKey]struct{}, len(users))
	result := make([]panel.OnlineUser, 0, len(users))
	for _, onlineUser := range users {
		ip := netutil.NormalizeIP(onlineUser.IP)
		if ip == "" {
			continue
		}
		key := onlineKey{uid: onlineUser.UID, ip: ip}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, panel.OnlineUser{UID: onlineUser.UID, IP: ip})
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].UID != result[j].UID {
			return result[i].UID < result[j].UID
		}
		return result[i].IP < result[j].IP
	})
	return result
}

// dedupeOnlineIPMapByIP normalizes and removes duplicate (uid, ip) pairs while
// keeping the per-user grouping intact.
func dedupeOnlineIPMapByIP(data map[int][]string) map[int][]string {
	type onlineKey struct {
		uid int
		ip  string
	}
	seen := make(map[onlineKey]struct{})
	result := make(map[int][]string, len(data))

	uids := make([]int, 0, len(data))
	for uid := range data {
		uids = append(uids, uid)
	}
	sort.Ints(uids)

	for _, uid := range uids {
		for _, ip := range data[uid] {
			ip = netutil.NormalizeIP(ip)
			if ip == "" {
				continue
			}
			key := onlineKey{uid: uid, ip: ip}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			result[uid] = append(result[uid], ip)
		}
		sort.Strings(result[uid])
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

func buildOnlineIPPayload(onlineUsers []panel.OnlineUser, _ []panel.UserInfo) map[int][]string {
	data := make(map[int][]string, len(onlineUsers))
	for _, onlineuser := range onlineUsers {
		if onlineuser.IP == "" {
			continue
		}
		data[onlineuser.UID] = append(data[onlineuser.UID], onlineuser.IP)
	}
	return buildOnlineIPMapPayload(data, nil)
}

func buildOnlineIPMapPayload(data map[int][]string, _ []panel.UserInfo) map[int][]string {
	if data == nil {
		return map[int][]string{}
	}

	deduped := dedupeOnlineIPMapByIP(data)
	payload := make(map[int][]string, len(deduped))
	for uid, ips := range deduped {
		if len(ips) == 0 {
			continue
		}
		payload[uid] = append([]string(nil), ips...)
	}
	return payload
}
