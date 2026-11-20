// (c) Siemens AG 2026
//
// SPDX-License-Identifier: MIT

package packetflix

import (
	"net/http"
	"time"
)

// CaptureOption configures a capture handler created by
// [NewCaptureHandlerWith].
type CaptureOption func(*captureHandler)

// Admission decides about a capture request before the capture handler
// upgrades it to a websocket connection.
//
// To refuse a request, an Admission writes an ordinary HTTP response, such as
// a 401 status, and returns false; the capture handler then neither upgrades
// the request nor reports any [CaptureEvent].
//
// To admit a request, an Admission returns true together with either nil, so
// that the capture handler parses the capture arguments from the request as
// usual, or the capture arguments to use instead. In the latter case, nothing
// from the request's query parameters or headers influences the capture;
// still, the capture handler refreshes a stale capture target from the
// discovery service and falls back to the “any” network interface the same
// way as for parsed arguments. The capture handler doesn't modify the
// returned arguments.
type Admission func(w http.ResponseWriter, req *http.Request) (args *Args, admitted bool)

// WithAdmission returns a [CaptureOption] that runs the passed [Admission] on
// every capture request before the websocket upgrade.
func WithAdmission(admit Admission) CaptureOption {
	return func(h *captureHandler) { h.admit = admit }
}

// WithObserver returns a [CaptureOption] that reports the lifecycle events of
// every admitted capture request to the passed observer. The observer is
// called synchronously from the capture handler, so it must not block.
func WithObserver(observe func(CaptureEvent)) CaptureOption {
	return func(h *captureHandler) { h.observe = observe }
}

// WithPingInterval returns a [CaptureOption] that sends websocket ping control
// messages at the passed interval on every capture websocket connection, from
// the websocket upgrade until either side begins the closing handshake. A
// non-positive interval sends no pings, as does the capture handler returned by
// [NewCaptureHandler].
//
// Pings keep connections alive that pass through reverse proxies closing
// upgraded connections when the proxied server sends nothing for some time,
// such as nginx with its “proxy_read_timeout”, while a capture produces no
// packet data. Clients answering pings with pongs is not required, and missing
// pongs don't end a capture. A ping that cannot be sent is handled as a broken
// websocket connection, terminating the capture.
func WithPingInterval(interval time.Duration) CaptureOption {
	return func(h *captureHandler) { h.pingInterval = interval }
}

// CaptureEventType tells which lifecycle event of a capture a [CaptureEvent]
// reports.
type CaptureEventType int

const (
	// CaptureStarted reports that the capture program has been started.
	CaptureStarted CaptureEventType = iota + 1
	// CaptureFailed reports that the capture could not be started. This is
	// the final event of the capture.
	CaptureFailed
	// CaptureEnded reports that a started capture has ended. This is the
	// final event of the capture.
	CaptureEnded
)

// CaptureEvent reports a lifecycle event of an admitted capture request: each
// such request results in either a single [CaptureFailed] event, or a
// [CaptureStarted] event followed by a [CaptureEnded] event.
type CaptureEvent struct {
	Type    CaptureEventType
	Request *http.Request // the original capture request.
	ConnID  string        // ID of the websocket connection, as logged.
	// Effective capture arguments after refreshing a stale target; nil if the
	// capture failed before the arguments were known.
	Args *Args
	// For CaptureFailed and CaptureEnded, the websocket close code and reason
	// of the first close, whether sent by packetflix, a [CaptureEvent.Stop],
	// or the client. If the connection broke instead, such as when a ping
	// could not be sent, Code is 1006 (abnormal closure) with the error as
	// the reason. Code is zero if the websocket upgrade failed, as the upgrade
	// then already responded with an HTTP error status.
	Code   int
	Reason string

	conn *WSConn
}

// Stop stops a started capture from the server side: it initiates a graceful
// websocket close with the passed close code and reason, and terminates the
// capture program. If the client doesn't acknowledge the close within the
// [ClosingDeadline], the connection is closed anyway. Stop does nothing if the
// capture is already ending or has ended, or if the event isn't a
// [CaptureStarted] event. Stop can be called from any goroutine, also after
// the observer has returned.
func (e CaptureEvent) Stop(code int, reason string) {
	if e.Type != CaptureStarted || e.conn == nil {
		return
	}
	e.conn.stop(code, reason)
}
