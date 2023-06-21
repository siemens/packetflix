// (c) Siemens AG 2023
//
// SPDX-License-Identifier: MIT

// A Grumbler collects the stderr output from the capture process and makes it
// available for later use in websocket close reason texts.
//
// Nota bene: Grumblers definitely do not spring from some famous saucerers'
// universe. But they might well fit into Terry Pratchett's "Disc World"
// universe.

package main

import (
	"strings"
)

// Grumbler collect the strerr reason message of a process.
type Grumbler struct {
	*WSConn         // so we can log based on a specific connection.
	r       *string // the first line of grumbling.
	frozenr bool    // ...do not gather any more reason.
}

// NewGrumbler returns a new grumbler collecting the error grumbling from a
// process.
func NewGrumbler(conn *WSConn) *Grumbler {
	return &Grumbler{
		WSConn: conn,
		r:      new(string),
	}
}

// Collects all the grumbling of a process into a gigantic buffer. And log it
// for debugging purposes. The grumbling later can be used to construct a
// reason text for the websocket close control message to give capture clients
// an indication as to www -- what went wrong. Please note that we expect
// grumbling to eventually be terminated by \n so we only log at these points.
func (g Grumbler) Write(grumble []byte) (n int, err error) {
	n = len(grumble)
	// As long as the reason string isn't yet frozen we keep adding grumbling
	// to it. The reason is that nsenter grumbles as multiple pieces until it
	// finally is finished. Oh, and wipe out those pesky \r that cause trouble
	// further down the road since we're removing \n anyway.
	grrr := strings.Replace(string(grumble), "\r", "", -1)
	for {
		if i := strings.IndexRune(grrr, '\n'); i >= 0 {
			// Looks like we've got now what could be a complete line...
			if !g.frozenr {
				*g.r += grrr[:i]
				// ...so check if its really something important that we could
				// use later as a reason text for a websocket close control
				// message, or just some murmuring.
				if !strings.HasPrefix(*g.r, StdErrCapturePrefix) {
					// Just mumbling, so throw it away and try to make
					// some sense of the rest...
					g.Debugf("capture process: %s", *g.r)
					*g.r = ""
					grrr = grrr[i+1:]
				} else {
					// First error message line complete, so log this also as
					// an error. Then process any "overspill" as ordinary
					// mumbling instead of an error. This way, the log will
					// contain all of the error message.
					g.frozenr = true
					g.Errorf("capture process: %s", *g.r)
					grrr = grrr[i+1:]
				}
			} else {
				// We've already a complete and thus frozen reason line, so we
				// now just log all other lines (mumble, mumble)...
				g.Debugf("capture process mumble: %s", grrr[:i])
				grrr = grrr[i+1:]
			}
		} else {
			// Still no complete line; that is, not terminated by \n. So
			// keep on collecting and wait for the next round. But only, if
			// we're not already frozen.
			if !g.frozenr {
				*g.r += grrr
			}
			break
		}
	}
	return
}

// Reason returns the grumbling reason why something failed miserably. It strips
// off the well-known prefixes from the capture and nsenter programs in order to
// keep the reason string short, because websocket close control message only
// allow for less than 128 octets. Looks like the ISO/OSI finally got their
// revenge by sneaking in their .CLOSE service primitives into websockets...!
func (g Grumbler) Reason() string {
	return strings.TrimPrefix(*(g.r), StdErrCapturePrefix)
}
