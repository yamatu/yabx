package hy2

import (
	"sync"

	"github.com/InazumaV/V2bX/conf"
	vCore "github.com/InazumaV/V2bX/core"
	"go.uber.org/zap"
)

var _ vCore.Core = (*Hysteria2)(nil)

type Hysteria2 struct {
	Hy2nodes map[string]Hysteria2node
	// nodesMu guards Hy2nodes. The map is written by AddNode/DelNode on the node
	// monitor goroutine while GetUserTraffic and the loggers read it from the
	// traffic report goroutine and from connection goroutines. An unsynchronized
	// map read racing a write is a fatal (unrecoverable) runtime error.
	nodesMu sync.RWMutex
	Auth    *V2bX
	Logger  *zap.Logger
}

// getNode returns a copy of the node registered for tag.
func (h *Hysteria2) getNode(tag string) (Hysteria2node, bool) {
	h.nodesMu.RLock()
	defer h.nodesMu.RUnlock()

	n, ok := h.Hy2nodes[tag]
	return n, ok
}

func (h *Hysteria2) setNode(tag string, n Hysteria2node) {
	h.nodesMu.Lock()
	defer h.nodesMu.Unlock()

	h.Hy2nodes[tag] = n
}

func (h *Hysteria2) deleteNode(tag string) {
	h.nodesMu.Lock()
	defer h.nodesMu.Unlock()

	delete(h.Hy2nodes, tag)
}

// nodesSnapshot copies the registered nodes so callers can iterate without
// holding the lock.
func (h *Hysteria2) nodesSnapshot() []Hysteria2node {
	h.nodesMu.RLock()
	defer h.nodesMu.RUnlock()

	nodes := make([]Hysteria2node, 0, len(h.Hy2nodes))
	for _, n := range h.Hy2nodes {
		nodes = append(nodes, n)
	}
	return nodes
}

func init() {
	vCore.RegisterCore("hysteria2", New)
}

func New(c *conf.CoreConfig) (vCore.Core, error) {
	loglever := "error"
	if c.Hysteria2Config.LogConfig.Level != "" {
		loglever = c.Hysteria2Config.LogConfig.Level
	}
	log, err := initLogger(loglever, "console")
	if err != nil {
		return nil, err
	}
	return &Hysteria2{
		Hy2nodes: make(map[string]Hysteria2node),
		Auth: &V2bX{
			usersMap: make(map[string]int),
		},
		Logger: log,
	}, nil
}

func (h *Hysteria2) Protocols() []string {
	return []string{
		"hysteria2",
	}
}

func (h *Hysteria2) Start() error {
	return nil
}

func (h *Hysteria2) Close() error {
	for _, n := range h.nodesSnapshot() {
		err := n.Hy2server.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func (h *Hysteria2) Type() string {
	return "hysteria2"
}
