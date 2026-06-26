package sing

import (
	"errors"
	"fmt"
	"sort"

	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/conf"
	vCore "github.com/InazumaV/V2bX/core"
	F "github.com/sagernet/sing/common/format"
)

type singInboundState struct {
	info   *panel.NodeInfo
	config *conf.Options
	users  map[string]panel.UserInfo
}

func (b *Sing) addSingNode(tag string, info *panel.NodeInfo, config *conf.Options) error {
	if _, err := getInboundOptions(tag, info, config, nil, nil); err != nil {
		return err
	}

	b.singMu.Lock()
	defer b.singMu.Unlock()

	b.singState[tag] = &singInboundState{
		info:   info,
		config: config,
		users:  make(map[string]panel.UserInfo),
	}
	return nil
}

func (b *Sing) deleteSingNode(tag string) bool {
	b.singMu.Lock()
	defer b.singMu.Unlock()

	_, found := b.singState[tag]
	delete(b.singState, tag)
	return found
}

func (b *Sing) addSingUsers(p *vCore.AddUsersParams) (int, error) {
	b.singMu.Lock()
	defer b.singMu.Unlock()

	state, found := b.singState[p.Tag]
	if !found {
		return 0, errors.New("the sing inbound state not found")
	}
	state.info = p.NodeInfo
	for _, user := range p.Users {
		state.users[user.Uuid] = user
	}
	if err := b.applySingInboundLocked(p.Tag, state); err != nil {
		return 0, err
	}
	return len(p.Users), nil
}

func (b *Sing) delSingUsers(users []panel.UserInfo, tag string) error {
	b.singMu.Lock()
	defer b.singMu.Unlock()

	state, found := b.singState[tag]
	if !found {
		return errors.New("the sing inbound state not found")
	}
	for _, user := range users {
		delete(state.users, user.Uuid)
	}
	return b.applySingInboundLocked(tag, state)
}

func (b *Sing) applySingInboundLocked(tag string, state *singInboundState) error {
	in := b.box.Inbound()
	if len(state.users) == 0 {
		if _, found := in.Get(tag); found {
			if err := in.Remove(tag); err != nil {
				return fmt.Errorf("delete inbound error: %s", err)
			}
		}
		return nil
	}

	inboundOptions, err := getInboundOptions(tag, state.info, state.config, buildSortedPanelUsers(state.users), nil)
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

func buildSortedPanelUsers(users map[string]panel.UserInfo) []panel.UserInfo {
	keys := make([]string, 0, len(users))
	for uuid := range users {
		keys = append(keys, uuid)
	}
	sort.Strings(keys)

	sortedUsers := make([]panel.UserInfo, 0, len(keys))
	for _, uuid := range keys {
		sortedUsers = append(sortedUsers, users[uuid])
	}
	return sortedUsers
}
