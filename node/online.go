package node

import (
	"github.com/InazumaV/V2bX/api/panel"
)

// getOnlineUsers returns the current online devices of this node.
//
// The limiter registry is the single source of truth for online devices: every
// core (xray / sing / hysteria2) records the client IP through CheckLimit on
// each connection, keyed by uid and normalized IP. Relying on a core specific
// probe (e.g. the xray stats OnlineMap) made the reported online count depend
// on the core and expire after ~20s even for long lived connections.
func (c *Controller) getOnlineUsers() ([]panel.OnlineUser, error) {
	if c.limiter == nil {
		return []panel.OnlineUser{}, nil
	}

	onlineUsers, err := c.limiter.GetOnlineDevice()
	if err != nil {
		return nil, err
	}
	if onlineUsers == nil {
		return []panel.OnlineUser{}, nil
	}

	return *onlineUsers, nil
}

func (c *Controller) getOnlineIPMap() (map[int][]string, error) {
	if c.limiter == nil {
		return map[int][]string{}, nil
	}

	return c.limiter.GetOnlineIPMap()
}
