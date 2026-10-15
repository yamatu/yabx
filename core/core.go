package core

import (
	"github.com/InazumaV/V2bX/conf"
)

type registration struct {
	new       func(c *conf.CoreConfig) (Core, error)
	protocols []string
}

var (
	cores = map[string]registration{}
)

// NewCore always returns a Selector, even for one core: the Selector fills in
// Options.Core for nodes that only name a CoreName (or nothing), which the cores
// themselves rely on.
func NewCore(c []conf.CoreConfig) (Core, error) {
	return NewSelector(c)
}

// RegisterCore registers a core type with the protocols it serves, so a core can
// be chosen for a node without creating it first.
func RegisterCore(t string, f func(c *conf.CoreConfig) (Core, error), protocols []string) {
	cores[t] = registration{new: f, protocols: protocols}
}

func RegisteredCore() []string {
	cs := make([]string, 0, len(cores))
	for k := range cores {
		cs = append(cs, k)
	}
	return cs
}
