package core

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/hashicorp/go-multierror"
	log "github.com/sirupsen/logrus"

	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/conf"
)

// coreEntry is one configured core. The core itself is created the first time a
// node is added to it, so a core no node uses costs nothing.
type coreEntry struct {
	// key is the Name of the core, or its Type when it has none.
	key    string
	typ    string
	config conf.CoreConfig
	core   Core
}

type Selector struct {
	mu      sync.Mutex
	entries []*coreEntry
	nodes   sync.Map
}

func NewSelector(c []conf.CoreConfig) (Core, error) {
	if len(c) == 0 {
		return nil, errors.New("no core is configured")
	}
	entries := make([]*coreEntry, 0, len(c))
	for _, t := range c {
		typ := strings.ToLower(t.Type)
		if _, ok := cores[typ]; !ok {
			return nil, errors.New("unknown core type: " + t.Type)
		}
		key := t.Name
		if key == "" {
			key = t.Type
		}
		entries = append(entries, &coreEntry{key: key, typ: typ, config: t})
	}
	return &Selector{entries: entries}, nil
}

// ownsInbounds reports whether the core config defines inbounds of its own,
// which have to listen even when no node is added to that core.
func ownsInbounds(c *conf.CoreConfig) bool {
	switch {
	case c.XrayConfig != nil && c.XrayConfig.InboundConfigPath != "":
		return true
	case c.SingConfig != nil && c.SingConfig.OriginalPath != "":
		return true
	}
	return false
}

func (s *Selector) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.entries {
		if !ownsInbounds(&e.config) {
			continue
		}
		if err := s.startLocked(e); err != nil {
			return err
		}
	}
	return nil
}

func (s *Selector) startLocked(e *coreEntry) error {
	if e.core != nil {
		return nil
	}
	c, err := cores[e.typ].new(&e.config)
	if err != nil {
		return fmt.Errorf("create core %s error: %w", e.key, err)
	}
	if err = c.Start(); err != nil {
		_ = c.Close()
		return fmt.Errorf("start core %s error: %w", e.key, err)
	}
	e.core = c
	log.WithField("core", e.key).Info("Core started")
	return nil
}

func (s *Selector) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var errs error
	for _, e := range s.entries {
		if e.core == nil {
			continue
		}
		if err := e.core.Close(); err != nil {
			errs = multierror.Append(errs, err)
		}
		e.core = nil
	}
	return errs
}

func isSupported(protocol string, protocols []string) bool {
	for i := range protocols {
		if protocol == protocols[i] {
			return true
		}
	}
	return false
}

// pickLocked chooses the core of a node. Among several candidates a running core
// wins, so nodes that do not name their core do not start a second one.
func (s *Selector) pickLocked(info *panel.NodeInfo, option *conf.Options) *coreEntry {
	if len(option.CoreName) > 0 {
		for _, e := range s.entries {
			if e.key == option.CoreName {
				return e
			}
		}
		return nil
	}
	var first *coreEntry
	for _, e := range s.entries {
		if len(option.Core) == 0 {
			if !isSupported(info.Type, cores[e.typ].protocols) {
				continue
			}
		} else if option.Core != e.typ {
			continue
		}
		if e.core != nil {
			return e
		}
		if first == nil {
			first = e
		}
	}
	return first
}

func (s *Selector) AddNode(tag string, info *panel.NodeInfo, option *conf.Options) error {
	s.mu.Lock()
	e := s.pickLocked(info, option)
	if e == nil {
		s.mu.Unlock()
		return errors.New("the node type is not support")
	}
	err := s.startLocked(e)
	core := e.core
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if len(option.Core) == 0 {
		option.Core = core.Type()
		err := option.UnmarshalJSON(option.RawOptions)
		if err != nil {
			return fmt.Errorf("unmarshal option error: %s", err)
		}
		option.RawOptions = nil
	}
	err = core.AddNode(tag, info, option)
	if err != nil {
		return err
	}
	s.nodes.Store(tag, core)
	return nil
}

func (s *Selector) DelNode(tag string) error {
	if t, e := s.nodes.Load(tag); e {
		err := t.(Core).DelNode(tag)
		if err != nil {
			return err
		}
		s.nodes.Delete(tag)
		return nil
	}
	return errors.New("the node is not have")
}

func (s *Selector) AddUsers(p *AddUsersParams) (added int, err error) {
	t, e := s.nodes.Load(p.Tag)
	if !e {
		return 0, errors.New("the node is not have")
	}
	return t.(Core).AddUsers(p)
}

func (s *Selector) GetUserTraffic(tag, uuid string, reset bool) (up int64, down int64) {
	t, e := s.nodes.Load(tag)
	if !e {
		return 0, 0
	}
	return t.(Core).GetUserTraffic(tag, uuid, reset)
}

func (s *Selector) RestoreUserTraffic(tag, uuid string, up, down int64) {
	t, e := s.nodes.Load(tag)
	if !e {
		return
	}
	t.(Core).RestoreUserTraffic(tag, uuid, up, down)
}

func (s *Selector) DelUsers(users []panel.UserInfo, tag string, info *panel.NodeInfo) error {
	t, e := s.nodes.Load(tag)
	if !e {
		return errors.New("the node is not have")
	}
	return t.(Core).DelUsers(users, tag, info)
}

func (s *Selector) Protocols() []string {
	protocols := make([]string, 0)
	for _, e := range s.entries {
		protocols = append(protocols, cores[e.typ].protocols...)
	}
	return protocols
}

func (s *Selector) Type() string {
	keys := make([]string, 0, len(s.entries))
	for _, e := range s.entries {
		keys = append(keys, e.key)
	}
	return "Selector(" + strings.Join(keys, " ") + ")"
}
