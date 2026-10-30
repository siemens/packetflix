// (c) Siemens AG 2023
//
// SPDX-License-Identifier: MIT

// Turns Linux kernel network namespace identifiers into filesystem references.

package packetflix

import (
	"fmt"
	"log/slog"
	"os"
	"syscall"
	"unicode"

	"github.com/thediveo/go-mntinfo"
	"github.com/thediveo/nonstd/xslog"
)

// netnsPath returns a filesystem reference to the network namespace in question.
// It searches both the processes as well as bindmount'ed network namespaces...
func netnsPath(netns uint64) string {
	if ref := processNetnsPath(netns); ref != "" {
		return ref
	}
	return bindmountNetnsPath(netns)
}

// processNetnsPath returns the /proc/[PID]/ns/net reference of the first
// process found to be attached to the network namespace in question, ignoring
// the fact that individual threads might be elsewhere.
func processNetnsPath(netns uint64) string {
	procpids, err := os.ReadDir("/proc")
	if err != nil {
		slog.Error("cannot read /proc", xslog.Error(err))
		return ""
	}
	for _, procpid := range procpids {
		// Skip all non-process entries and aliases in /proc.
		if !procpid.IsDir() || !unicode.IsDigit(rune(procpid.Name()[0])) {
			continue
		}
		if ref := fmt.Sprintf("/proc/%s/ns/net", procpid.Name()); isNetns(ref, netns) {
			return ref
		}
	}
	return ""
}

// bindmountNetnsPath returns the first bind mount of the network namespace in
// question.
func bindmountNetnsPath(netns uint64) string {
	for _, mount := range mntinfo.MountsOfType(-1, "nsfs") {
		if isNetns(mount.MountPoint, netns) {
			return mount.MountPoint
		}
	}
	return ""
}

// isNetns returns true if the specified filesystem path references the network
// namespace in question.
func isNetns(ref string, netns uint64) bool {
	finfo, err := os.Stat(ref)
	if err != nil {
		return false
	}
	stat, ok := finfo.Sys().(*syscall.Stat_t)
	return ok && stat.Ino == netns
}
