// (c) Siemens AG 2023
//
// SPDX-License-Identifier: MIT

// A Piper writes the packet capture data stream fed into it via stdin from
// the packet capture process to the associated websocket connection.

package packetflix

import (
	"github.com/gorilla/websocket"
	"github.com/thediveo/nonstd/xslog"
)

// Piper pipes all packet capture output straight into a websocket connection.
type Piper struct {
	*WSConn
	Failed bool // true after websocket connection failed
}

// NewPiper returns a new piper working on the specified (wrapped) websocket
// connection.
func NewPiper(conn *WSConn) *Piper {
	return &Piper{
		WSConn: conn,
	}
}

// Write pipes all packet data received into our associated websocket. If the
// websocket is closing or broken, then Write silently drops the data, as the
// websocket watcher then terminates the capture process anyway; failing here
// would otherwise make a normal end of capture look like a capture process
// failure.
func (p *Piper) Write(data []byte) (n int, err error) {
	if err := p.WriteMessage(websocket.BinaryMessage, data); err != nil && !p.Failed {
		p.log.Debug("websocket closing or broken, dropping capture data", xslog.Error(err))
		p.Failed = true
	}
	return len(data), nil
}
