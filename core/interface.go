package core

import (
	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/conf"
)

type AddUsersParams struct {
	Tag   string
	Users []panel.UserInfo
	*panel.NodeInfo
}

type Core interface {
	Start() error
	Close() error
	AddNode(tag string, info *panel.NodeInfo, config *conf.Options) error
	DelNode(tag string) error
	AddUsers(p *AddUsersParams) (added int, err error)
	GetUserTraffic(tag, uuid string, reset bool) (up int64, down int64)
	// RestoreUserTraffic puts usage back into the user's counters. It is called
	// when a traffic report did not reach the panel: the counters are reset when
	// they are read, so without a restore the usage of that round would be lost
	// (never billed, and the user's quota never consumed). See node/user.go.
	RestoreUserTraffic(tag, uuid string, up, down int64)
	DelUsers(users []panel.UserInfo, tag string, info *panel.NodeInfo) error
	Protocols() []string
	Type() string
}
