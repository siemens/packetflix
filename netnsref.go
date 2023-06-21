// (c) Siemens AG 2023
//
// SPDX-License-Identifier: MIT

// Turns Linux kernel network namespace identifiers into filesystem references.

package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"syscall"
	"unicode"

	log "github.com/sirupsen/logrus"
	"github.com/thediveo/go-mntinfo"
)

// netnsPath returns a filesystem reference to the network namespace in question.
// It searches both the processes as well as bindmount'ed network namespaces...
func netnsPath(netns uint64) string {
	// First search in processes for a suitable netns filesystem reference...
	procpids, err := ioutil.ReadDir("/proc")
	if err == nil {
		for _, procpid := range procpids {
			// Skip all non-process entries and aliases in /proc.
			if r := rune(procpid.Name()[0]); !procpid.IsDir() || !unicode.IsDigit(r) {
				continue
			}
			// See which network namespace this process is in -- ignoring
			// the fact that individual threads might be elsewhere...
			ref := fmt.Sprintf("/proc/%s/ns/net", procpid.Name())
			finfo, err := os.Stat(ref)
			if err != nil {
				continue
			}
			stat, ok := finfo.Sys().(*syscall.Stat_t)
			if !ok {
				continue
			}
			if stat.Ino == netns {
				return ref
			}
		}
	} else {
		log.Errorf("/proc failed: %s", err.Error())
	}
	// Then search bindmount'ed network namespaces for a match...
	for _, mount := range mntinfo.MountsOfType(-1, "nsfs") {
		finfo, err := os.Stat(mount.MountPoint)
		if err != nil {
			continue
		}
		stat, ok := finfo.Sys().(*syscall.Stat_t)
		if !ok {
			continue
		}
		if stat.Ino == netns {
			return mount.MountPoint
		}
	}
	// Nope. Fail. Nothing found here.
	return ""
}
