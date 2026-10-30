// (c) Siemens AG 2023
//
// SPDX-License-Identifier: MIT

// A Grumbler collects the stderr output from the capture process and makes it
// available for later use in websocket close reason texts.
//
// Nota bene: Grumblers definitely do not spring from some famous saucerers'
// universe. But they might well fit into Terry Pratchett's "Disc World"
// universe.

package packetflix

import (
	"log/slog"
	"strings"
)

// Grumbler collect the strerr reason message of a process.
type Grumbler struct {
	*WSConn        // so we can log based on a specific connection.
	r       string // the first line of grumbling.
	frozenr bool   // ...do not gather any more reason.
}

// NewGrumbler returns a new grumbler collecting the error grumbling from a
// process.
func NewGrumbler(conn *WSConn) *Grumbler {
	return &Grumbler{
		WSConn: conn,
	}
}

// Write collects all the grumbling of a process into a gigantic buffer. And
// log it for debugging purposes. The grumbling later can be used to construct a
// reason text for the websocket close control message to give capture clients
// an indication as to www -- what went wrong. Please note that we expect
// grumbling to eventually be terminated by \n so we only log at these points.
func (g *Grumbler) Write(grumble []byte) (int, error) {
	// Wipe out those pesky \r that cause trouble further down the road since
	// we're removing \n anyway.
	grrr := strings.ReplaceAll(string(grumble), "\r", "")
	for {
		line, rest, complete := strings.Cut(grrr, "\n")
		if !complete {
			// Not terminated by \n, so keep on collecting (unless already
			// frozen) and wait for the next round.
			if !g.frozenr {
				g.r += line
			}
			return len(grumble), nil
		}
		g.grumbleLine(line)
		grrr = rest
	}
}

// grumbleLine handles a complete line of grumbling: the first line starting
// with the capture program's error prefix becomes the frozen reason and gets
// logged as an error, all other lines are just mumbling.
func (g *Grumbler) grumbleLine(line string) {
	if g.frozenr {
		g.log.Debug("capture process mumble", slog.String("stderr", line))
		return
	}
	g.r += line
	if !strings.HasPrefix(g.r, StdErrCapturePrefix) {
		g.log.Debug("capture process", slog.String("stderr", g.r))
		g.r = ""
		return
	}
	g.frozenr = true
	g.log.Error("capture process", slog.String("stderr", g.r))
}

// Reason returns the grumbling reason why something failed miserably. It strips
// off the well-known prefixes from the capture and nsenter programs in order to
// keep the reason string short, because websocket close control message only
// allow for less than 128 octets. Looks like the ISO/OSI finally got their
// revenge by sneaking in their .CLOSE service primitives into websockets...!
func (g *Grumbler) Reason() string {
	return strings.TrimPrefix(g.r, StdErrCapturePrefix)
}
