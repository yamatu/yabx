package hy2

import (
	"net"
	"sync"

	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/common/counter"
	vCore "github.com/InazumaV/V2bX/core"
	"github.com/apernet/hysteria/core/v2/server"
)

var _ server.Authenticator = &V2bX{}

type V2bX struct {
	usersMap map[string]int
	mutex    sync.Mutex
}

func (v *V2bX) Authenticate(addr net.Addr, auth string, tx uint64) (ok bool, id string) {
	v.mutex.Lock()
	defer v.mutex.Unlock()
	if _, exists := v.usersMap[auth]; exists {
		return true, auth
	}
	return false, ""
}

// AddUsers and DelUsers update the shared user map in one pass.
//
// Every user used to be written by its own goroutine, which for a node with
// thousands of users meant thousands of goroutines (and a WaitGroup) competing
// for the same mutex to do a single map write.
func (h *Hysteria2) AddUsers(p *vCore.AddUsersParams) (added int, err error) {
	h.Auth.mutex.Lock()
	defer h.Auth.mutex.Unlock()

	for _, user := range p.Users {
		h.Auth.usersMap[user.Uuid] = user.Id
	}
	return len(p.Users), nil
}

func (h *Hysteria2) DelUsers(users []panel.UserInfo, tag string, _ *panel.NodeInfo) error {
	h.Auth.mutex.Lock()
	defer h.Auth.mutex.Unlock()

	for _, user := range users {
		delete(h.Auth.usersMap, user.Uuid)
	}
	return nil
}

func (h *Hysteria2) RestoreUserTraffic(tag string, uuid string, up, down int64) {
	node, ok := h.getNode(tag)
	if !ok {
		return
	}
	logger, ok := node.TrafficLogger.(*HookServer)
	if !ok || logger == nil {
		return
	}
	if v, ok := logger.Counter.Load(tag); ok {
		v.(*counter.TrafficCounter).Restore(uuid, up, down)
	}
}

func (h *Hysteria2) GetUserTraffic(tag string, uuid string, reset bool) (up int64, down int64) {
	node, ok := h.getNode(tag)
	if !ok {
		return 0, 0
	}
	logger, ok := node.TrafficLogger.(*HookServer)
	if !ok || logger == nil {
		return 0, 0
	}
	if v, ok := logger.Counter.Load(tag); ok {
		c := v.(*counter.TrafficCounter)
		up = c.GetUpCount(uuid)
		down = c.GetDownCount(uuid)
		if reset {
			c.Reset(uuid)
		}
		return up, down
	}
	return 0, 0
}
