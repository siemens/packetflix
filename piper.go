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

// Write pipes all packet data received into our associated websocket. If there
// is a problem writing, then we simply close the websocket connection, because
// it is already in error state, so further writes including control messages
// aren't possible anymore.
func (p *Piper) Write(data []byte) (n int, err error) {
	err = p.WriteMessage(websocket.BinaryMessage, data)
	if err != nil {
		p.log.Debug("websocket broken", xslog.Error(err))
		p.Failed = true
	}
	n = len(data)
	return
}
