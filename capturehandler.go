// (c) Siemens AG 2023
//
// SPDX-License-Identifier: MIT

package packetflix

import (
	"fmt"
	"log/slog"
	"net/http"
	"os/exec"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	pcapng "github.com/siemens/csharg/v2/pcapng"
	"github.com/thediveo/caps/v2"
	"github.com/thediveo/lxkns/model"
	"github.com/thediveo/lxkns/ops"
	"github.com/thediveo/lxkns/ops/portable"
	"github.com/thediveo/lxkns/species"
	"github.com/thediveo/nonstd/xslog"
	"golang.org/x/sys/unix"
)

// captureHandler handles the /capture API endpoint. This upgrades the
// connection to a websocket and then checks the request parameters (which might
// be in the headers in order to work around a bug in the Kubernetes pod proxy
// verb of the remote API.)
type captureHandler struct {
	discovery string // "host:port" address of the discovery service.
	program   string // capture program to run.
	upgrader  websocket.Upgrader
	admit     Admission          // optional admission before upgrading.
	observe   func(CaptureEvent) // optional capture lifecycle observer.
}

// NewCaptureHandler returns a handler for the /capture API endpoint that
// refreshes stale capture target information from the discovery service at
// the specified "host:port" address.
func NewCaptureHandler(discovery string) http.Handler {
	return NewCaptureHandlerWith(discovery)
}

// NewCaptureHandlerWith returns a handler for the /capture API endpoint that
// refreshes stale capture target information from the discovery service at
// the specified "host:port" address, configured by the passed options. Without
// any options, it behaves the same as the handler returned by
// [NewCaptureHandler].
func NewCaptureHandlerWith(discovery string, opts ...CaptureOption) http.Handler {
	h := &captureHandler{discovery: discovery, program: CaptureProgram}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// ServeHTTP serves a single capture stream over a websocket connection.
func (h *captureHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	started := time.Now()
	conn := NewWSConn()
	conn.log.Debug("handler started", slog.String("path", req.URL.Path))
	defer func() {
		conn.log.Debug("handler finished", slog.String("path", req.URL.Path),
			slog.Duration("duration", time.Since(started).Truncate(time.Second)))
	}()

	var supplied *Args
	if h.admit != nil {
		var admitted bool
		if supplied, admitted = h.admit(w, req); !admitted {
			conn.log.Debug("capture request not admitted")
			return
		}
	}

	// Upgrade the HTTP connection into a websocket connection, if possible.
	// Otherwise, bail out as we will handle further only a correct websocket
	// connection.
	conn.log.Debug("beginning websocket upgrade")
	cnx, err := h.upgrader.Upgrade(w, req, nil)
	if err != nil {
		conn.log.Error("websocket upgrade failed", xslog.Error(err))
		h.notify(CaptureFailed, req, conn, nil, 0, "websocket upgrade failed")
		return
	}
	conn.Conn = cnx
	// Because I seem to not trust my coding abilities... ;)
	defer func() {
		_ = conn.Close()
	}()
	conn.log.Debug("websocket upgrade successful")

	var args *Args
	if supplied != nil {
		args, err = resolveSuppliedArgs(supplied, conn, h.discovery)
	} else {
		args, err = parseCaptureParams(req, conn, h.discovery)
	}
	if err != nil {
		conn.log.Error("invalid capture parameters", xslog.Error(err))
		h.fail(req, conn, nil, websocket.ClosePolicyViolation, err.Error())
		return
	}
	h.capture(req, conn, args)
}

// notify reports a capture lifecycle event to the observer, if any.
func (h *captureHandler) notify(typ CaptureEventType, req *http.Request, conn *WSConn, args *Args, code int, reason string) {
	if h.observe == nil {
		return
	}
	h.observe(CaptureEvent{
		Type:    typ,
		Request: req,
		ConnID:  conn.ID,
		Args:    args,
		Code:    code,
		Reason:  reason,
		conn:    conn,
	})
}

// fail reports the failed capture and then gracefully closes the websocket
// connection with the passed code and reason. It reports first, as the client
// controls how long the closing handshake takes.
func (h *captureHandler) fail(req *http.Request, conn *WSConn, args *Args, code int, reason string) {
	h.notify(CaptureFailed, req, conn, args, code, reason)
	conn.GracefullyClose(code, reason)
}

// capture runs the capture program with the passed capture arguments,
// streaming its packet capture data over the websocket connection until either
// the capture program terminates or the websocket connection closes.
func (h *captureHandler) capture(req *http.Request, conn *WSConn, args *Args) {
	target := args.Target
	conn.log.Debug("capture target", slog.Int("netns", target.NetNS), slog.Int("pid", target.Pid))
	netnsref := netnsPath(uint64(target.NetNS))
	if len(netnsref) == 0 {
		reason := "could not locate network namespace for container"
		conn.log.Error(reason, slog.Int("netns", target.NetNS))
		h.fail(req, conn, args, websocket.ClosePolicyViolation, reason)
		return
	}
	conn.log.Debug("capturing from network interfaces",
		slog.String("type", target.Type), slog.Any("nifs", target.NetworkInterfaces))
	if len(args.CaptureFilter) > 0 {
		conn.log.Debug("filtering capture", slog.Int("filter_len", len(args.CaptureFilter)))
	}
	if args.KeepChaste {
		conn.log.Debug("avoiding promiscuous mode, if possible, on all network interfaces")
	}
	conn.log.Debug("referencing netns", slog.Int("netns", target.NetNS), slog.String("path", netnsref))

	// With the target information we can now create a "portable namespace
	// reference": it allows us to open, validate and "lock" a namespace based
	// on the supplied information, as well as use the locked reference to
	// switch a separate Go routine starting dumpcap into the desired network
	// namespace. "Locked" here means that we keep a file descriptor reference
	// open to the network namespace, so it cannot vanish between the time we
	// validate it and we later switch into it.
	portref := portable.PortableReference{
		ID:        species.NamespaceIDfromInode(uint64(target.NetNS)),
		Type:      species.CLONE_NEWNET,
		PID:       model.PIDType(target.Pid),
		Starttime: uint64(target.StartTime),
	}
	lockednetns, unlocker, err := portref.Open()
	if err != nil {
		conn.log.Error("cannot lock netns", slog.Int("netns", target.NetNS), xslog.Error(err))
		h.fail(req, conn, args, websocket.CloseInternalServerErr, "cannot lock target network stack")
		return
	}
	defer unlocker()

	// Wire the capture command's stdout to the websocket, and sneak in a pcapng
	// stream editor to inject capture target meta information.
	cmd := exec.Command(h.program, dumpcapArgs(args)...)
	piper := NewPiper(conn)
	cmd.Stdout = pcapng.NewStreamEditor(piper, target, args.CaptureFilter, args.KeepChaste)
	grrr := NewGrumbler(conn)
	cmd.Stderr = grrr

	// With everything prepared as far as we can, we now run the packet
	// capture command (including network namespace changeover) and wait for
	// it to terminate, for good or bad.
	conn.log.Debug("starting capture command")
	res, err := ops.Execute(
		func() any {
			// This will be executed in a separate OS-locked Go routine,
			// switched into the target's network namespace. dumpcap thus will
			// run attached to the target's network stack. All animal magic
			// thanks to lxkns ;)
			netnsid, _ := ops.NamespacePath(fmt.Sprintf("/proc/%d/ns/net", unix.Gettid())).ID()
			conn.log.Debug("running capture program inside locked netns",
				slog.String("program", h.program), slog.Uint64("netns", netnsid.Ino))
			// In order to run the unprivileged dumpcap binary with the
			// necessary capabilities, we need to transfer them via the ambient
			// capabilities. If you don't know what ambient capabilities are,
			// then you really don't want to know anyway.
			if err := SetAmbient(caps.CAP_NET_ADMIN, caps.CAP_NET_RAW); err != nil {
				conn.log.Error("cannot set ambient capabilities", xslog.Error(err))
			}
			return cmd.Start()
		},
		lockednetns)
	if err != nil {
		conn.log.Error("cannot switch to target network stack", xslog.Error(err))
		h.fail(req, conn, args, websocket.CloseInternalServerErr, "cannot switch to target network stack")
		return
	}
	err, _ = res.(error)
	if err != nil {
		conn.log.Error("cannot start capture process", xslog.Error(err))
		h.fail(req, conn, args, websocket.CloseInternalServerErr, "cannot start capture process")
		return
	}
	// So far, so good. While things can still go south from here on, we passed
	// at least the first few hurdles...
	conn.Process = cmd.Process
	h.notify(CaptureStarted, req, conn, args, 0, "")
	var wg sync.WaitGroup
	wg.Add(2)
	// The watcher/reader go routine will terminate after the websocket
	// connection has been closed (gracefully or not) and it will terminate
	// the capture process if it hasn't terminated by now on its own.
	go func() {
		defer wg.Done()
		conn.Watch()
	}()
	// The waiter go routine waits for the capture process to terminate. It
	// then will try to initiate a graceful close ... which will fail and be
	// ignored if the websocket is already closing or closed. But if the
	// process terminated while the websocket is still fine, this will
	// report the termination cause to the client and then close the websocket.
	go func() {
		defer wg.Done()
		err := cmd.Wait()
		if err == nil {
			conn.log.Debug("capture process terminated successfully")
			conn.InitiateGracefulClose(websocket.CloseNormalClosure, "capture process terminated")
			return
		}
		conn.log.Error("capture process failure", xslog.Error(err))
		switch reason := grrr.Reason(); {
		case reason != "":
			conn.InitiateGracefulClose(websocket.CloseInternalServerErr, reason)
		case piper.Failed:
			conn.InitiateGracefulClose(websocket.CloseNormalClosure, "ciao")
		default:
			conn.InitiateGracefulClose(websocket.CloseInternalServerErr, "capture process failed")
		}
	}()
	// Wait for both the watcher/reader and the process waiter to finish.
	wg.Wait()
	conn.finish()
	code, reason := conn.closeCause()
	h.notify(CaptureEnded, req, conn, args, code, reason)
}

// dumpcapArgs returns the capture program's CLI arguments for the specified
// capture arguments.
func dumpcapArgs(args *Args) []string {
	captargs := []string{
		// write packet capture stream to stdout.
		"-w", "-",
		// use pcapng format anyway.
		"-F", "pcapng",
		// keep (almost) quiet: no packet capture count, but still some initial messages :(
		"-q",
	}
	// "Avoid promiscuous mode" and the capture filter only become defaults for
	// all network interfaces when they precede the first -i.
	if args.KeepChaste {
		captargs = append(captargs, "-p")
	}
	if args.CaptureFilter != "" {
		captargs = append(captargs, "-f", args.CaptureFilter)
	}
	for _, nif := range args.NetworkInterfaces {
		captargs = append(captargs, "-i", nif)
	}
	return captargs
}
