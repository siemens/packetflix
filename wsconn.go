// (c) Siemens AG 2023
//
// SPDX-License-Identifier: MIT

// Wraps a server-side websocket connection with its own human-readable unique
// ID. This helps to clearly map log debug and error messages to their
// respective websocket connections, thus keeping them clearly separated.
// Additionally, we also associate the capturing process (if any) with this
// connection, so we can sanely manage it.

package packetflix

import (
	"log/slog"
	"os"
	"sync"
	"syscall"
	"time"

	petname "github.com/dustinkirkland/golang-petname"
	"github.com/gorilla/websocket"
	"github.com/thediveo/nonstd/xslog"
)

// Things are tricky, since we have to juggle with an external process that can
// terminate or needs to be terminated, and a websocket connection that can
// error, be closed, request closing (from client) and close its side so that
// the client also closes.
//
// 1. process terminates after start (websocket open): we then need to carry out
// a graceful websocket close -- but only if the websocket is still open and not
// already closing.
//    - note to self: graceful close in progress.
//    - send close control message, informing the client about the process
//      termination reason (mutex'd with piper writer).
//    - wait for client's close control message (in websocket watcher).
//    - close websocket.
//
// 2. process fails to start (websocket open): we then need to carry out a
// graceful websocket close -- but only if the websocket is still open and not
// already closing.
//    - note to self: graceful close in progress.
//    - send close control message, informing the client about the process
//      failure reason (mutex'd with piper writer).
//    - wait for client's close control message (in websocket watcher).
//    - close websocket.
//
// 3. client closes: we then need to acknowlege the close and terminate the
// process -- please note that there's no graceful close in progress at the time
// we receive the client's close.
//    - note to self: graceful ack in progress.
//    - terminate process (if not already done so).
//    - send close control message (generic "ciao").
//    - close websocket.
//
// 4. websocket write error: as this will trigger 5. (see next) anyway and sets
// things in motion, we can just keep tucking on here, dumping any data to be
// written, but not balking either.
//
// 5. websocket read(er) error: we can only close/terminate.
//    - note to self: broken/closed.
//    - terminate process (if not already done so).
//    - close websocket.
//    - terminate reader go routine.

// WSConnState ...
type WSConnState int

const (
	// WSConnOpen declares the websocket connection being still open.
	WSConnOpen WSConnState = iota
	// WSConnClosing declares the websocket connection being in the handshake
	// for a graceful close.
	WSConnClosing
	// WSConnClosed declares the websocket connection being closed.
	WSConnClosed
)

// WSConn is a websocket connection with a unique, human-friendly ID. This
// allows differentiating multiple (concurrent) websocket connections in the
// logs.
type WSConn struct {
	state           WSConnState // what's up???
	*websocket.Conn             // usual (gorilla) websocket connection.
	ID              string      // unique ID string for this connection.
	*os.Process                 // associated process with its lifetime bounded by this connection.
	terminateOnce   sync.Once
	log             *slog.Logger // logger tagged with this connection's ID.
}

// NewWSConn returns a new websocket connection wrapper that features an
// additional ID, so multiple (concurrent) websocket connections can still be
// differentiated in the logs.
func NewWSConn() *WSConn {
	id := petname.Generate(2, "-")
	return &WSConn{ID: id, log: slog.With(slog.String("conn", id))}
}

// Terminate sends the associated capture process the signal to terminate
// itself. It ensures that this signal is sent only once, even when triggering
// this method multiple times.
func (c *WSConn) Terminate() {
	if c.Process != nil {
		c.terminateOnce.Do(func() {
			c.log.Debug("signalling capture process to terminate")
			if err := c.Signal(syscall.SIGTERM); err != nil {
				c.log.Debug("cannot signal capture process", xslog.Error(err))
			}
		})
	}
}

// Watch watches the websocket connection for any signs of closing or failure.
// Additionally, it also handles acknowledging a graceful shutdown or receiving
// a client's graceful acknowledge.
func (c *WSConn) Watch() {
	c.log.Debug("watching websocket connection")
	for {
		_, _, err := c.ReadMessage()
		if err != nil {
			if cerr, ok := err.(*websocket.CloseError); ok {
				// It is not an error, but instead a close control message by
				// the client. We now need to see if we need to acknowledge it
				// or if it was the final close message in the handshake...
				switch c.state {
				case WSConnOpen:
					// Let's try to gracefully acknowledge the close, and then
					// we're done.
					c.state = WSConnClosed
					c.log.Debug("capture client closing",
						slog.Int("code", cerr.Code), slog.String("reason", cerr.Text))
					c.log.Debug("acknowledging close (ciao!)")
					_ = c.SetWriteDeadline(time.Now().Add(ClosingDeadline))
					_ = c.WriteMessage(
						websocket.CloseMessage,
						websocket.FormatCloseMessage(cerr.Code, "ciao"))
				case WSConnClosing:
					// It is already the final ack, so we're done now too.
					c.state = WSConnClosed
					c.log.Debug("capture client acknowledged close",
						slog.Int("code", cerr.Code), slog.String("reason", cerr.Text))
				}
			}
			// Any error means that the websocket is broken, and any close means
			// that we're done by now. So release resources.
			c.Terminate()
			c.log.Debug("websocket closed")
			_ = c.Close()
			return
		}
		// Whatever the websocket client is sending us ... we'll ignore it. And
		// we need to keep listening in order to correctly process incomming
		// control messages.
	}
}

// InitiateGracefulClose initiates a graceful close handshake. It immediately
// returns after kicking off the close procedure. This will then cause the
// websocket reader to finish the closing handshake and finally terminating the
// capture process. If there is a problem to initiate the closing procedure,
// then the websocket will be closed immediately and the capture process
// terminated.
func (c *WSConn) InitiateGracefulClose(code int, reason string) {
	if c.state == WSConnOpen {
		c.log.Debug("beginning graceful websocket connection close",
			slog.Int("code", code), slog.String("reason", reason))
		_ = c.SetWriteDeadline(time.Now().Add(ClosingDeadline))
		c.state = WSConnClosing
		err := c.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(code, reason))
		if err != nil {
			c.state = WSConnClosed
			c.log.Error("sending graceful close control message failed", xslog.Error(err))
			c.Terminate()
			_ = c.Close()
		}
	}
}

// GracefullyClose runs a complete graceful close handshake and only returns
// after this has completed or completely failed. Use this convenience method
// when there is yet no process to also wait for or to terminate. Otherwise, use
// asynchronous InitiateGracefulClose because there's already a Watch() on this
// websocket as well as a Wait() on the capture process running in parallel.
func (c *WSConn) GracefullyClose(code int, reason string) {
	if c.state == WSConnOpen {
		c.InitiateGracefulClose(code, reason)
		c.Watch()
	}
}
