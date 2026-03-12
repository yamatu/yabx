package node

import (
	"github.com/InazumaV/V2bX/api/panel"
	vCore "github.com/InazumaV/V2bX/core"
)

func (c *Controller) getOnlineUsers() ([]panel.OnlineUser, error) {
	if reporter, ok := c.server.(vCore.OnlineUserReporter); ok {
		return reporter.GetOnlineUsers(c.tag, c.userList)
	}

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
	if reporter, ok := c.server.(vCore.OnlineUserReporter); ok {
		return reporter.GetOnlineIPMap(c.tag, c.userList)
	}

	if c.limiter == nil {
		return map[int][]string{}, nil
	}

	return c.limiter.GetOnlineIPMap()
}
