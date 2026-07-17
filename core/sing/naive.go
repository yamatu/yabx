package sing

import (
	"errors"
	"fmt"
	"sort"

	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/conf"
	vCore "github.com/InazumaV/V2bX/core"
	"github.com/sagernet/sing/common/auth"
	F "github.com/sagernet/sing/common/format"
)

type naiveInboundState struct {
	info   *panel.NodeInfo
	config *conf.Options
	users  map[string]panel.UserInfo
}

func (b *Sing) addNaiveNode(tag string, info *panel.NodeInfo, config *conf.Options) {
	b.naiveMu.Lock()
	defer b.naiveMu.Unlock()

	b.naiveState[tag] = &naiveInboundState{
		info:   info,
		config: config,
		users:  make(map[string]panel.UserInfo),
	}
}

func (b *Sing) hasNaiveNode(tag string) bool {
	b.naiveMu.Lock()
	defer b.naiveMu.Unlock()

	_, found := b.naiveState[tag]
	return found
}

func (b *Sing) deleteNaiveNode(tag string) {
	b.naiveMu.Lock()
	defer b.naiveMu.Unlock()

	delete(b.naiveState, tag)
}

func (b *Sing) addNaiveUsers(p *vCore.AddUsersParams) (int, error) {
	b.naiveMu.Lock()
	defer b.naiveMu.Unlock()

	state, found := b.naiveState[p.Tag]
	if !found {
		return 0, errors.New("the naive inbound state not found")
	}
	state.info = p.NodeInfo
	for _, user := range p.Users {
		state.users[user.Uuid] = user
	}
	if err := b.applyNaiveInboundLocked(p.Tag, state); err != nil {
		return 0, err
	}
	return len(p.Users), nil
}

func (b *Sing) delNaiveUsers(users []panel.UserInfo, tag string) error {
	b.naiveMu.Lock()
	defer b.naiveMu.Unlock()

	state, found := b.naiveState[tag]
	if !found {
		return errors.New("the naive inbound state not found")
	}
	for _, user := range users {
		delete(state.users, user.Uuid)
	}
	return b.applyNaiveInboundLocked(tag, state)
}

func (b *Sing) applyNaiveInboundLocked(tag string, state *naiveInboundState) error {
	in := b.box.Inbound()
	if len(state.users) == 0 {
		if _, found := in.Get(tag); found {
			if err := in.Remove(tag); err != nil {
				return fmt.Errorf("delete inbound error: %s", err)
			}
		}
		return nil
	}

	inboundOptions, err := getInboundOptions(tag, state.info, state.config, buildNaiveAuthUsers(state.users))
	if err != nil {
		return err
	}
	if _, found := in.Get(tag); found {
		if err = in.Remove(tag); err != nil {
			return fmt.Errorf("delete inbound error: %s", err)
		}
	}
	err = in.Create(
		b.ctx,
		b.box.Router(),
		b.logFactory.NewLogger(F.ToString("inbound/", inboundOptions.Type, "[", tag, "]")),
		tag,
		inboundOptions.Type,
		inboundOptions.Options,
	)
	if err != nil {
		return fmt.Errorf("add inbound error: %s", err)
	}
	return nil
}

func buildNaiveAuthUsers(users map[string]panel.UserInfo) []auth.User {
	keys := make([]string, 0, len(users))
	for uuid := range users {
		keys = append(keys, uuid)
	}
	sort.Strings(keys)

	authUsers := make([]auth.User, 0, len(keys))
	for _, uuid := range keys {
		authUsers = append(authUsers, auth.User{
			Username: uuid,
			Password: uuid,
		})
	}
	return authUsers
}
