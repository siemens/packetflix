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
	"errors"
	"log/slog"
	"net"
	"os"
	"strings"
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
//      termination reason (WriteControl, so concurrently with piper writes).
//    - wait for client's close control message (in websocket watcher).
//    - close websocket.
//
// 2. process fails to start (websocket open): we then need to carry out a
// graceful websocket close -- but only if the websocket is still open and not
// already closing.
//    - note to self: graceful close in progress.
//    - send close control message, informing the client about the process
//      failure reason (WriteControl, so concurrently with piper writes).
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
	*websocket.Conn        // usual (gorilla) websocket connection.
	ID              string // unique ID string for this connection.
	*os.Process            // associated process with its lifetime bounded by this connection.
	terminateOnce   sync.Once
	log             *slog.Logger // logger tagged with this connection's ID.

	// The watcher, the process waiter, and server-side stops all move the
	// connection through its states concurrently.
	mu          sync.Mutex
	state       WSConnState // what's up??? Guarded by mu.
	closeCode   int         // close code of the first close; guarded by mu.
	closeReason string      // close reason of the first close; guarded by mu.
	finished    bool        // capture handler is done with this connection; guarded by mu.
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
	if c.Process == nil {
		return
	}
	c.terminateOnce.Do(func() {
		c.log.Debug("signalling capture process to terminate")
		if err := c.Signal(syscall.SIGTERM); err != nil {
			c.log.Debug("cannot signal capture process", xslog.Error(err))
		}
	})
}

// Watch watches the websocket connection for any signs of closing or failure.
// Additionally, it also handles acknowledging a graceful shutdown or receiving
// a client's graceful acknowledge.
func (c *WSConn) Watch() {
	c.log.Debug("watching websocket connection")
	// Whatever the websocket client is sending us ... we'll ignore it. But we
	// need to keep listening in order to correctly process incoming control
	// messages.
	var err error
	for err == nil {
		_, _, err = c.ReadMessage()
	}
	if cerr, ok := errors.AsType[*websocket.CloseError](err); ok {
		c.handleClose(cerr)
	} else {
		c.mu.Lock()
		c.recordClose(websocket.CloseAbnormalClosure, err.Error())
		c.mu.Unlock()
	}
	// Any error means that the websocket is broken, and any close means that
	// we're done by now. So release resources.
	c.Terminate()
	c.log.Debug("websocket closed")
	_ = c.Close()
}

// handleClose handles a close control message by the client: we either need to
// acknowledge it, or it is the final close message in the handshake we
// started.
func (c *WSConn) handleClose(cerr *websocket.CloseError) {
	c.mu.Lock()
	state := c.state
	if state != WSConnClosed {
		c.state = WSConnClosed
	}
	c.recordClose(cerr.Code, cerr.Text)
	c.mu.Unlock()
	switch state {
	case WSConnOpen:
		c.log.Debug("capture client closing",
			slog.Int("code", cerr.Code), slog.String("reason", cerr.Text))
		c.log.Debug("acknowledging close (ciao!)")
		_ = c.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(cerr.Code, "ciao"),
			time.Now().Add(ClosingDeadline))
	case WSConnClosing:
		c.log.Debug("capture client acknowledged close",
			slog.Int("code", cerr.Code), slog.String("reason", cerr.Text))
	}
}

// InitiateGracefulClose initiates a graceful close handshake. It immediately
// returns after kicking off the close procedure. This will then cause the
// websocket reader to finish the closing handshake and finally terminating the
// capture process. If there is a problem to initiate the closing procedure,
// then the websocket will be closed immediately and the capture process
// terminated.
func (c *WSConn) InitiateGracefulClose(code int, reason string) {
	c.initiateGracefulClose(code, reason)
}

// initiateGracefulClose implements InitiateGracefulClose, returning true if it
// successfully initiated a graceful close handshake.
func (c *WSConn) initiateGracefulClose(code int, reason string) bool {
	// Close frame payloads are limited to 125 octets, including the 2 octet
	// code, and must be valid UTF-8.
	if len(reason) > 123 {
		reason = reason[:123]
	}
	reason = strings.ToValidUTF8(reason, "")
	c.mu.Lock()
	if c.state != WSConnOpen {
		c.mu.Unlock()
		return false
	}
	c.state = WSConnClosing
	c.recordClose(code, reason)
	c.mu.Unlock()
	c.log.Debug("beginning graceful websocket connection close",
		slog.Int("code", code), slog.String("reason", reason))
	err := c.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(code, reason),
		time.Now().Add(ClosingDeadline))
	if err == nil {
		return true
	}
	c.mu.Lock()
	c.state = WSConnClosed
	c.mu.Unlock()
	c.log.Error("sending graceful close control message failed", xslog.Error(err))
	c.Terminate()
	_ = c.Close()
	return false
}

// stop stops the capture from the server side, unless the capture handler is
// already done with this connection: it initiates a graceful close and
// terminates the capture process. As a client might not acknowledge the close,
// it also bounds the remaining closing handshake to the ClosingDeadline.
func (c *WSConn) stop(code int, reason string) {
	c.mu.Lock()
	finished := c.finished
	c.mu.Unlock()
	if finished || !c.initiateGracefulClose(code, reason) {
		return
	}
	// Unlike gorilla's SetReadDeadline, the underlying connection's deadline
	// can be set concurrently with the watcher's reads.
	_ = c.UnderlyingConn().SetReadDeadline(time.Now().Add(ClosingDeadline))
	c.Terminate()
}

// finish marks the capture handler being done with this connection.
func (c *WSConn) finish() {
	c.mu.Lock()
	c.finished = true
	c.mu.Unlock()
}

// startPinging sends websocket ping control messages at the passed interval
// while the connection is open, returning an idempotent function that stops
// pinging and waits for the pinger to have finished. The pinger also finishes
// on its own as soon as a closing handshake begins or the connection is gone.
// A ping that cannot be sent breaks the connection, terminating the capture
// process.
func (c *WSConn) startPinging(interval time.Duration) (stop func()) {
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if !c.ping() {
					return
				}
			}
		}
	}()
	return sync.OnceFunc(func() {
		close(done)
		<-finished
	})
}

// ping sends a single websocket ping control message, returning false if the
// connection isn't open anymore.
func (c *WSConn) ping() bool {
	// Holding the lock while sending the ping keeps it from slipping in after a
	// close control message.
	c.mu.Lock()
	if c.state != WSConnOpen {
		c.mu.Unlock()
		return false
	}
	err := c.WriteControl(websocket.PingMessage, nil, time.Now().Add(ClosingDeadline))
	if err == nil {
		c.mu.Unlock()
		return true
	}
	if errors.Is(err, net.ErrClosed) {
		// The connection is already gone, so there's nothing left to do.
		c.mu.Unlock()
		return false
	}
	c.state = WSConnClosed
	c.recordClose(websocket.CloseAbnormalClosure, "cannot send ping")
	c.mu.Unlock()
	c.log.Error("cannot send ping, closing websocket", xslog.Error(err))
	c.Terminate()
	_ = c.Close()
	return false
}

// recordClose records the passed close code and reason, unless there already
// has been an earlier close. Must be called with mu locked.
func (c *WSConn) recordClose(code int, reason string) {
	if c.closeCode != 0 {
		return
	}
	c.closeCode, c.closeReason = code, reason
}

// closeCause returns the close code and reason of the first close, whether
// initiated by us or the client, or zero and an empty reason if the
// connection hasn't been closed yet.
func (c *WSConn) closeCause() (int, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeCode, c.closeReason
}

// GracefullyClose runs a complete graceful close handshake and only returns
// after this has completed or completely failed. Use this convenience method
// when there is yet no process to also wait for or to terminate. Otherwise, use
// asynchronous InitiateGracefulClose because there's already a Watch() on this
// websocket as well as a Wait() on the capture process running in parallel.
func (c *WSConn) GracefullyClose(code int, reason string) {
	c.mu.Lock()
	state := c.state
	c.mu.Unlock()
	if state != WSConnOpen {
		return
	}
	c.InitiateGracefulClose(code, reason)
	c.Watch()
}
