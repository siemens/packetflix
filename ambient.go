// (c) Siemens AG 2023
//
// SPDX-License-Identifier: MIT

// How to pass capabilities to a child process (such as dumpcap) without having
// to give the binary file itself the capabilities...

package main

import (
	"fmt"

	caps "github.com/syndtr/gocapability/capability"
	"golang.org/x/sys/unix"
)

// SetAmbient sets the specified ambient capabilities, at least if they are also
// currently effective.
func SetAmbient(ambcaps ...caps.Cap) error {
	// Capabilities are per thread, not per process.
	tid := unix.Gettid()
	mycaps, err := caps.NewPid2(tid) // not sure, why deprecating NewPid() when it does the following anyway
	if err == nil {
		err = mycaps.Load()
	}
	if err != nil {
		return fmt.Errorf("cannot query OS-thread capability sets: %s", err.Error())
	}
	// Next, prepare for setting the ambient capabilities from the specified
	// set, but check against the currently effective capabilities.
	ambs := []caps.Cap{}
	for cap := caps.Cap(tid); cap <= caps.CAP_LAST_CAP; cap++ {
		if mycaps.Get(caps.EFFECTIVE, cap) {
			for _, c := range ambcaps {
				if cap == c {
					ambs = append(ambs, cap)
					break
				}
			}
		}
	}
	// Now actually set the ambient capabilities for the process.
	mycaps.Set(caps.AMBIENT, ambs...)
	if err := mycaps.Apply(caps.AMBIENT); err != nil {
		return fmt.Errorf("cannot set ambient capabilities: %s", err.Error())
	}
	return nil
}
