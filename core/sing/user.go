package sing

import (
	"encoding/base64"

	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/common/counter"
	"github.com/InazumaV/V2bX/core"
	"github.com/sagernet/sing-box/option"
)

func (b *Sing) AddUsers(p *core.AddUsersParams) (added int, err error) {
	if p.NodeInfo.Type == "naive" {
		return b.addNaiveUsers(p)
	}
	return b.addSingUsers(p)
}

func (b *Sing) GetUserTraffic(tag, uuid string, reset bool) (up int64, down int64) {
	if v, ok := b.hookServer.counter.Load(tag); ok {
		c := v.(*counter.TrafficCounter)
		up = c.GetUpCount(uuid)
		down = c.GetDownCount(uuid)
		if reset {
			c.Reset(uuid)
		}
		return
	}
	return 0, 0
}

func (b *Sing) DelUsers(users []panel.UserInfo, tag string, info *panel.NodeInfo) error {
	if info.Type == "naive" {
		return b.delNaiveUsers(users, tag)
	}
	return b.delSingUsers(users, tag)
}

func buildVLESSUsers(users []panel.UserInfo, flow string) []option.VLESSUser {
	vlessUsers := make([]option.VLESSUser, len(users))
	for i, user := range users {
		vlessUsers[i] = option.VLESSUser{
			Name: user.Uuid,
			Flow: flow,
			UUID: user.Uuid,
		}
	}
	return vlessUsers
}

func buildVMessUsers(users []panel.UserInfo) []option.VMessUser {
	vmessUsers := make([]option.VMessUser, len(users))
	for i, user := range users {
		vmessUsers[i] = option.VMessUser{
			Name: user.Uuid,
			UUID: user.Uuid,
		}
	}
	return vmessUsers
}

func buildShadowsocksUsers(users []panel.UserInfo, cipher string) []option.ShadowsocksUser {
	ssUsers := make([]option.ShadowsocksUser, len(users))
	for i, user := range users {
		ssUsers[i] = option.ShadowsocksUser{
			Name:     user.Uuid,
			Password: shadowsocksUserPassword(user.Uuid, cipher),
		}
	}
	return ssUsers
}

func shadowsocksUserPassword(uuid string, cipher string) string {
	switch cipher {
	case "2022-blake3-aes-128-gcm":
		return base64.StdEncoding.EncodeToString([]byte(limitString(uuid, 16)))
	case "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305":
		return base64.StdEncoding.EncodeToString([]byte(limitString(uuid, 32)))
	default:
		return uuid
	}
}

func limitString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

func buildTrojanUsers(users []panel.UserInfo) []option.TrojanUser {
	trojanUsers := make([]option.TrojanUser, len(users))
	for i, user := range users {
		trojanUsers[i] = option.TrojanUser{
			Name:     user.Uuid,
			Password: user.Uuid,
		}
	}
	return trojanUsers
}

func buildTUICUsers(users []panel.UserInfo) []option.TUICUser {
	tuicUsers := make([]option.TUICUser, len(users))
	for i, user := range users {
		tuicUsers[i] = option.TUICUser{
			Name:     user.Uuid,
			UUID:     user.Uuid,
			Password: user.Uuid,
		}
	}
	return tuicUsers
}

func buildAnyTLSUsers(users []panel.UserInfo) []option.AnyTLSUser {
	anyTLSUsers := make([]option.AnyTLSUser, len(users))
	for i, user := range users {
		anyTLSUsers[i] = option.AnyTLSUser{
			Name:     user.Uuid,
			Password: user.Uuid,
		}
	}
	return anyTLSUsers
}

func buildHysteriaUsers(users []panel.UserInfo) []option.HysteriaUser {
	hysteriaUsers := make([]option.HysteriaUser, len(users))
	for i, user := range users {
		hysteriaUsers[i] = option.HysteriaUser{
			Name:       user.Uuid,
			AuthString: user.Uuid,
		}
	}
	return hysteriaUsers
}

func buildHysteria2Users(users []panel.UserInfo) []option.Hysteria2User {
	hysteria2Users := make([]option.Hysteria2User, len(users))
	for i, user := range users {
		hysteria2Users[i] = option.Hysteria2User{
			Name:     user.Uuid,
			Password: user.Uuid,
		}
	}
	return hysteria2Users
}
