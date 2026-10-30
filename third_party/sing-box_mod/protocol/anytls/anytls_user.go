package anytls

import (
	"net"

	anytls "github.com/anytls/sing-anytls"
	"github.com/sagernet/sing-box/option"
)

// AddUsers 批量添加用户
// users: 用户信息列表
// ids: 用户ID列表
// 返回错误信息
func (h *Inbound) AddUsers(users []option.AnyTLSUser, ids []int) error {
	for i, user := range users {
		h.userNameList = append(h.userNameList, user.Password)
		h.uuidToUid[user.Password] = ids[i]
		h.uidToUuid[ids[i]] = user.Password
	}

	// 构建 anytls.User 列表
	userList := make([]anytls.User, len(h.userNameList))
	for i, uuid := range h.userNameList {
		userList[i] = anytls.User{Name: uuid, Password: uuid}
	}

	h.service.UpdateUsers(userList)
	return nil
}

// DelUsers 批量删除用户
// names: 待删除用户名列表
// 返回错误信息
func (h *Inbound) DelUsers(names []string) error {
	if len(names) == 0 {
		return nil
	}

	toDelete := make(map[string]struct{})
	for _, name := range names {
		toDelete[name] = struct{}{}
		delete(h.uidToUuid, h.uuidToUid[name])
		delete(h.uuidToUid, name)
		h.userconns.Range(func(key, value interface{}) bool {
			if value.(string) == name {
				key.(net.Conn).Close()
				h.userconns.Delete(key)
			}
			return true
		})
	}

	remaining := make([]string, 0, len(h.userNameList))
	for _, user := range h.userNameList {
		if _, found := toDelete[user]; !found {
			remaining = append(remaining, user)
		}
	}

	h.userNameList = remaining
	// 构建 anytls.User 列表
	userList := make([]anytls.User, len(h.userNameList))
	for i, uuid := range h.userNameList {
		userList[i] = anytls.User{Name: uuid, Password: uuid}
	}
	h.service.UpdateUsers(userList)
	return nil
}
