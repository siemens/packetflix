// (c) Siemens AG 2023
//
// SPDX-License-Identifier: MIT

// A Piper writes the packet capture data stream fed into it via stdin from
// the packet capture process to the associated websocket connection.

package main

import (
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
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

// Pipes all packet data received into our associated websocket. If there is a
// problem writing, then we simply close the websocket connection, because it
// is already in error state, so further writes including control messages
// aren't possible anymore.
func (p *Piper) Write(data []byte) (n int, err error) {
	err = p.WriteMessage(websocket.BinaryMessage, data)
	if err != nil {
		log.Debugf("websocket broken: %s", err.Error())
		p.Failed = true
	}
	n = len(data)
	return
}
