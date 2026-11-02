// (c) Siemens AG 2023
//
// SPDX-License-Identifier: MIT

// How to pass capabilities to a child process (such as dumpcap) without having
// to give the binary file itself the capabilities...

package packetflix

import (
	"fmt"

	"github.com/thediveo/caps/v2"
	"golang.org/x/sys/unix"
)

// SetAmbient raises the specified capabilities in the ambient set of the
// calling OS thread, so that they get passed on to binaries without file
// capabilities that are exec'ed from this thread. The calling go routine must
// be locked to its OS thread and the capabilities must be permitted.
func SetAmbient(capnos ...int) error {
	if len(capnos) == 0 {
		return nil
	}
	taskcaps, err := caps.OfCurrentTask()
	if err != nil {
		return fmt.Errorf("cannot query OS-thread capability sets: %w", err)
	}
	for _, capno := range capnos {
		if !taskcaps.Permitted().Has(capno) {
			return fmt.Errorf("capability %s not permitted", caps.CapabilityNameByNumber[capno])
		}
	}
	// The kernel only allows raising ambient capabilities that are both
	// permitted and inheritable.
	if _, err := taskcaps.Inheritable().Add(capnos[0], capnos[1:]...).ApplyToCurrentTask(); err != nil {
		return fmt.Errorf("cannot set inheritable capabilities: %w", err)
	}
	for _, capno := range capnos {
		if err := unix.Prctl(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_RAISE, uintptr(capno), 0, 0); err != nil {
			return fmt.Errorf("cannot raise ambient capability %s: %w",
				caps.CapabilityNameByNumber[capno], err)
		}
	}
	return nil
}
