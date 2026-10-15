package core

import (
	"testing"

	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/conf"
)

type fakeCore struct {
	typ     string
	started bool
	closed  bool
}

var fakeCreated []*fakeCore

func (f *fakeCore) Start() error                                         { f.started = true; return nil }
func (f *fakeCore) Close() error                                         { f.closed = true; return nil }
func (f *fakeCore) AddNode(string, *panel.NodeInfo, *conf.Options) error { return nil }
func (f *fakeCore) DelNode(string) error                                 { return nil }
func (f *fakeCore) AddUsers(*AddUsersParams) (int, error)                { return 0, nil }
func (f *fakeCore) GetUserTraffic(string, string, bool) (int64, int64)   { return 0, 0 }
func (f *fakeCore) RestoreUserTraffic(string, string, int64, int64)      {}
func (f *fakeCore) DelUsers([]panel.UserInfo, string, *panel.NodeInfo) error {
	return nil
}
func (f *fakeCore) Protocols() []string { return cores[f.typ].protocols }
func (f *fakeCore) Type() string        { return f.typ }

func registerFake(t *testing.T, typ string, protocols ...string) {
	t.Helper()
	RegisterCore(typ, func(*conf.CoreConfig) (Core, error) {
		c := &fakeCore{typ: typ}
		fakeCreated = append(fakeCreated, c)
		return c, nil
	}, protocols)
	t.Cleanup(func() { delete(cores, typ) })
}

func newFakeSelector(t *testing.T, cfgs ...conf.CoreConfig) *Selector {
	t.Helper()
	fakeCreated = nil
	registerFake(t, "fakea", "vless", "trojan")
	registerFake(t, "fakeb", "vless", "tuic")
	c, err := NewCore(cfgs)
	if err != nil {
		t.Fatalf("NewCore: %v", err)
	}
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return c.(*Selector)
}

func TestSelectorCreatesCoresOnFirstUse(t *testing.T) {
	s := newFakeSelector(t,
		conf.CoreConfig{Type: "fakea"},
		conf.CoreConfig{Type: "fakeb", Name: "b1"},
		conf.CoreConfig{Type: "fakeb", Name: "b2"},
	)
	if len(fakeCreated) != 0 {
		t.Fatalf("%d cores were created before any node", len(fakeCreated))
	}

	tuic := &panel.NodeInfo{Type: "tuic"}
	if err := s.AddNode("n1", tuic, &conf.Options{Core: "fakeb"}); err != nil {
		t.Fatalf("AddNode n1: %v", err)
	}
	if err := s.AddNode("n2", tuic, &conf.Options{Core: "fakeb"}); err != nil {
		t.Fatalf("AddNode n2: %v", err)
	}
	if len(fakeCreated) != 1 || !fakeCreated[0].started {
		t.Fatalf("two nodes of one type created %d cores, want one started core", len(fakeCreated))
	}

	// A node without Core/CoreName lands on the running core that supports it.
	if err := s.AddNode("n3", &panel.NodeInfo{Type: "vless"}, &conf.Options{RawOptions: []byte(`{}`)}); err != nil {
		t.Fatalf("AddNode n3: %v", err)
	}
	if len(fakeCreated) != 1 {
		t.Fatalf("a protocol picked node started another core (%d cores)", len(fakeCreated))
	}

	if err := s.AddNode("n4", tuic, &conf.Options{CoreName: "b2", RawOptions: []byte(`{}`)}); err != nil {
		t.Fatalf("AddNode n4: %v", err)
	}
	if err := s.AddNode("n5", &panel.NodeInfo{Type: "trojan"}, &conf.Options{CoreName: "fakea", RawOptions: []byte(`{}`)}); err != nil {
		t.Fatalf("AddNode n5: %v", err)
	}
	if len(fakeCreated) != 3 {
		t.Fatalf("created %d cores, want 3", len(fakeCreated))
	}

	if err := s.AddNode("n6", tuic, &conf.Options{CoreName: "missing"}); err == nil {
		t.Fatal("a node naming a missing core was added")
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	for _, c := range fakeCreated {
		if !c.closed {
			t.Fatalf("core %s was not closed", c.typ)
		}
	}
}

func TestSelectorStartsCoresWithOwnInboundsEagerly(t *testing.T) {
	newFakeSelector(t,
		conf.CoreConfig{Type: "fakea", XrayConfig: &conf.XrayConfig{InboundConfigPath: "/etc/V2bX/custom_inbound.json"}},
		conf.CoreConfig{Type: "fakeb"},
	)
	if len(fakeCreated) != 1 || fakeCreated[0].typ != "fakea" || !fakeCreated[0].started {
		t.Fatalf("the core with its own inbounds was not started up front: %+v", fakeCreated)
	}
}

func TestSelectorRejectsUnknownAndEmptyConfigs(t *testing.T) {
	if _, err := NewCore(nil); err == nil {
		t.Fatal("an empty core list was accepted")
	}
	if _, err := NewCore([]conf.CoreConfig{{Type: "nope"}}); err == nil {
		t.Fatal("an unknown core type was accepted")
	}
}
