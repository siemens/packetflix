// (c) Siemens AG 2026
//
// SPDX-License-Identifier: MIT

package main

import (
	"runtime/debug"
	"strings"
)

const modulePath = "github.com/siemens/packetflix"

// version is only ever set at link time for builds lacking VCS information,
// such as Docker builds: -ldflags "-X main.version=…".
var version string

// buildVersion returns the semantic version of the packetflix module, as
// either set at link time or otherwise recorded by the Go toolchain in the
// binary's build information. It returns “(devel)” if no version is
// available.
func buildVersion() string {
	if version != "" {
		return strings.TrimPrefix(version, "v")
	}
	bi, _ := debug.ReadBuildInfo()
	return moduleVersion(bi)
}

// moduleVersion returns the packetflix module version from the passed build
// information, without any leading “v”.
func moduleVersion(bi *debug.BuildInfo) string {
	const devel = "(devel)"
	if bi == nil {
		return devel
	}
	for _, mod := range append([]*debug.Module{&bi.Main}, bi.Deps...) {
		if mod.Path != modulePath {
			continue
		}
		if mod.Replace != nil {
			mod = mod.Replace
		}
		if mod.Version == "" || mod.Version == devel {
			return devel
		}
		return strings.TrimPrefix(mod.Version, "v")
	}
	return devel
}
