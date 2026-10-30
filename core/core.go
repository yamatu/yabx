package core

import (
	"github.com/InazumaV/V2bX/conf"
)

type registration struct {
	new       func(c *conf.CoreConfig) (Core, error)
	protocols []string
}

var (
	cores        = map[string]registration{}
	coreVersions = map[string]string{}
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

// RegisterCoreVersion records the release version a core reports about itself,
// for kernels whose module version is a pseudo-version (xray tags v26.x.y but
// publishes the module as v1.2xxxxx).
func RegisterCoreVersion(t, version string) {
	coreVersions[t] = version
}

func RegisteredCoreVersions() map[string]string {
	return coreVersions
}

func RegisteredCore() []string {
	cs := make([]string, 0, len(cores))
	for k := range cores {
		cs = append(cs, k)
	}
	return cs
}
