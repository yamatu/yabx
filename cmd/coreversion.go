package cmd

import (
	"runtime/debug"
	"sort"

	vCore "github.com/InazumaV/V2bX/core"
)

// coreModules maps the name a core registers itself with to the module that
// carries its version. The version is read from the build info of the running
// binary instead of a constant, so it always names the kernel this build really
// links, including a replacement such as the sing-box fork.
var coreModules = map[string]string{
	"hysteria2": "github.com/apernet/hysteria/core/v2",
	"sing":      "github.com/sagernet/sing-box",
	"xray":      "github.com/xtls/xray-core",
}

// installedCoreVersions returns one "<core> <version>" line per kernel that is
// compiled into this binary, sorted by core name. Conditionally compiled cores
// are skipped, so an xray-only build does not claim to carry sing-box. It is
// printed by `V2bX version` and shown by the management menu in V2bX.sh.
func installedCoreVersions() []string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return nil
	}
	return coreVersionLines(info.Deps, vCore.RegisteredCore(), vCore.RegisteredCoreVersions())
}

// coreVersionLines is the pure part of installedCoreVersions: it only reports
// cores that are both registered and present in the module list. A version a
// core reports about itself wins over the module version.
func coreVersionLines(deps []*debug.Module, registered []string, reported map[string]string) []string {
	versions := make(map[string]string, len(deps))
	for _, dep := range deps {
		if dep == nil {
			continue
		}
		versions[dep.Path] = dep.Version
		// The replacement is the module the linker actually used. A directory
		// replacement has no version of its own, so keep the required one.
		if dep.Replace != nil && dep.Replace.Version != "" && dep.Replace.Version != "(devel)" {
			versions[dep.Path] = dep.Replace.Version
		}
	}

	lines := make([]string, 0, len(coreModules))
	for _, name := range registered {
		module, known := coreModules[name]
		if !known {
			continue
		}
		version := versions[module]
		if version == "" {
			continue
		}
		if v := reported[name]; v != "" {
			version = v
		}
		lines = append(lines, name+" "+version)
	}
	sort.Strings(lines)
	return lines
}
