package xray

import (
	"fmt"
	"sort"

	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/common/format"
	"github.com/InazumaV/V2bX/common/netutil"
)

const onlineStatSuffix = ">>>online"

func userOnlineStatName(tag, uuid string) string {
	return "user>>>" + format.UserTag(tag, uuid) + onlineStatSuffix
}

func inboundOnlineStatName(tag string) string {
	return "inbound>>>" + tag + onlineStatSuffix
}

func (c *Xray) GetOnlineUsers(tag string, users []panel.UserInfo) ([]panel.OnlineUser, error) {
	if c.shm == nil {
		return nil, fmt.Errorf("xray stats manager is not initialized")
	}

	onlineUsers := make([]panel.OnlineUser, 0)
	for _, user := range users {
		onlineMap := c.shm.GetOnlineMap(userOnlineStatName(tag, user.Uuid))
		if onlineMap == nil || onlineMap.Count() == 0 {
			continue
		}

		seen := make(map[string]struct{})
		for _, ip := range onlineMap.List() {
			ip = netutil.NormalizeIP(ip)
			if ip == "" {
				continue
			}
			if _, ok := seen[ip]; ok {
				continue
			}
			seen[ip] = struct{}{}
			onlineUsers = append(onlineUsers, panel.OnlineUser{UID: user.Id, IP: ip})
		}
	}

	sort.Slice(onlineUsers, func(i, j int) bool {
		if onlineUsers[i].UID != onlineUsers[j].UID {
			return onlineUsers[i].UID < onlineUsers[j].UID
		}
		return onlineUsers[i].IP < onlineUsers[j].IP
	})

	return onlineUsers, nil
}

func (c *Xray) GetOnlineIPMap(tag string, users []panel.UserInfo) (map[int][]string, error) {
	onlineUsers, err := c.GetOnlineUsers(tag, users)
	if err != nil {
		return nil, err
	}

	data := make(map[int][]string)
	for _, onlineUser := range onlineUsers {
		data[onlineUser.UID] = append(data[onlineUser.UID], onlineUser.IP)
	}

	return data, nil
}
