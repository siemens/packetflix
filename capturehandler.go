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
	upgrader  websocket.Upgrader
}

// NewCaptureHandler returns a handler for the /capture API endpoint that
// refreshes stale capture target information from the discovery service at
// the specified "host:port" address.
func NewCaptureHandler(discovery string) http.Handler {
	return &captureHandler{discovery: discovery}
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

	// Upgrade the HTTP connection into a websocket connection, if possible.
	// Otherwise, bail out as we will handle further only a correct websocket
	// connection.
	conn.log.Debug("beginning websocket upgrade")
	cnx, err := h.upgrader.Upgrade(w, req, nil)
	if err != nil {
		conn.log.Error("websocket upgrade failed", xslog.Error(err))
		return
	}
	conn.Conn = cnx
	// Because I seem to not trust my coding abilities... ;)
	defer func() {
		_ = conn.Close()
	}()
	conn.log.Debug("websocket upgrade successful")

	args, err := parseCaptureParams(req, conn, h.discovery)
	if err != nil {
		conn.log.Error("invalid capture parameters", xslog.Error(err))
		conn.GracefullyClose(websocket.ClosePolicyViolation, err.Error())
		return
	}
	target := args.Target
	conn.log.Debug("capture target", slog.String("type", target.Type), slog.Any("target", *target))
	netnsref := netnsPath(uint64(target.NetNS))
	if len(netnsref) == 0 {
		reason := "could not locate network namespace for container"
		conn.log.Error(reason, slog.Int("netns", target.NetNS))
		conn.GracefullyClose(websocket.ClosePolicyViolation, reason)
		return
	}
	conn.log.Debug("capturing from network interfaces",
		slog.String("type", target.Type), slog.Any("nifs", target.NetworkInterfaces))
	if len(args.CaptureFilter) > 0 {
		conn.log.Debug("filtering capture", slog.String("filter", args.CaptureFilter))
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
		conn.GracefullyClose(websocket.CloseInternalServerErr, "cannot lock target network stack")
		return
	}
	defer unlocker()
	// With the target network namespace under lock and key, prepare the
	// nsenter/packet capture commands (and their arguments) we want to start
	// soon.
	captargs := []string{
		// write packet capture stream to stdout.
		"-w", "-",
		// use pcapng format anyway.
		"-F", "pcapng",
		// keep (almost) quiet: no packet capture count, but still some initial messages :(
		"-q",
	}
	// As we want to apply "avoid promiscuous mode" to all network interfaces, we need
	// to specify it *before* the list of network interfaces.
	if args.KeepChaste {
		conn.log.Debug("avoiding promiscuous mode, if possible, on all network interfaces")
		captargs = append(captargs, "-p")
	}
	for _, nif := range target.NetworkInterfaces {
		captargs = append(captargs, "-i", nif)
	}
	if len(args.CaptureFilter) > 0 {
		conn.log.Debug("capture filter", slog.String("filter", args.CaptureFilter))
		captargs = append(captargs, "-f", args.CaptureFilter)
	}

	// Wire the capture command's stdout to the websocket, and sneak in a pcapng
	// stream editor to inject capture target meta information.
	cmd := exec.Command(CaptureProgram, captargs...)
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
				slog.String("program", CaptureProgram), slog.Uint64("netns", netnsid.Ino))
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
		conn.InitiateGracefulClose(websocket.CloseInternalServerErr, "cannot switch to target network stack")
		conn.Watch() // finishes the graceful close.
		return
	}
	err, _ = res.(error)
	if err != nil {
		conn.log.Error("cannot start capture process", xslog.Error(err))
		conn.InitiateGracefulClose(websocket.CloseInternalServerErr, "cannot start capture process")
		conn.Watch() // finishes the graceful close.
		return
	}
	// So far, so good. While things can still go south from here on, we passed
	// at least the first few hurdles...
	conn.Process = cmd.Process
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
		if err != nil {
			conn.log.Error("capture process failure", xslog.Error(err))
			r := grrr.Reason()
			if len(r) == 0 && !piper.Failed {
				r = "capture process failed"
			}
			if len(r) == 0 {
				conn.InitiateGracefulClose(websocket.CloseNormalClosure, "ciao")
			} else {
				conn.InitiateGracefulClose(websocket.CloseInternalServerErr, r)
			}
		} else {
			conn.log.Debug("capture process terminated successfully")
			conn.InitiateGracefulClose(websocket.CloseNormalClosure, "capture process terminated")
		}
	}()
	// Wait for both the watcher/reader and the process waiter to finish.
	wg.Wait()
}
