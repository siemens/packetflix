// (c) Siemens AG 2023
//
// SPDX-License-Identifier: MIT

// Packetflix is a capture-as-a-service implementation: it captures network
// packets from the network interfaces of a container, pod, process-less virtual
// IP stack, et cetera. The captured network packets are then streamed live via
// a websocket connection to clients connecting to our service.
//
// If available, then Packetflix will use GhostWire to update stale container
// references with the most up-to-date information as to capture correctly from
// pods and containers which were reloaded, et cetera. For this, run GhostWire
// side-by-side with Packetflix.
package main

import (
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	pcapng "github.com/siemens/csharg/pcapng"
	"golang.org/x/sys/unix"

	"github.com/gorilla/websocket"
	caps "github.com/syndtr/gocapability/capability"
	"github.com/thediveo/lxkns/model"
	"github.com/thediveo/lxkns/ops"
	"github.com/thediveo/lxkns/ops/portable"
	"github.com/thediveo/lxkns/species"
)

var wsupgrader = websocket.Upgrader{}

// Handle the /capture API endpoint. This upgrades the connection to a
// websocket and then checks the request parameters (which might be in the
// headers in order to work around a bug in the Kubernetes pod proxy verb of
// the remote API.)
func captureHandler(w http.ResponseWriter, req *http.Request) {
	started := time.Now()
	conn := NewWSConn()
	conn.Debugf("%s URL path handler started...", req.URL.Path)
	defer func() {
		d := time.Since(started)
		h := d / time.Hour
		d -= h * time.Hour
		mm := d / time.Minute
		d -= mm * time.Minute
		ss := d / time.Second
		conn.Debugf("%s handler finished after %d:%02d:%02d",
			req.URL.Path, h, mm, ss)
	}()

	// Upgrade the HTTP connection into a websocket connection, if possible.
	// Otherwise, bail out as we will handle further only a correct websocket
	// connection.
	conn.Debugf("beginning websocket upgrade...")
	cnx, err := wsupgrader.Upgrade(w, req, nil)
	if err != nil {
		conn.Errorf("websocket upgrade process failed: %s", err.Error())
		return
	}
	conn.Conn = cnx
	// Because I seem to not trust my coding abilities... ;)
	defer func() {
		conn.Close()
	}()
	conn.Debugf("websocket upgrade successful")

	args, err := parseCaptureParams(req, conn)
	if err != nil {
		conn.Errorf(err.Error())
		conn.GracefullyClose(websocket.CloseAbnormalClosure, err.Error())
		return
	}
	target := args.Target
	conn.Debugf("%s to capture from: %#v", target.Type, *target)
	netnsref := netnsPath(uint64(target.NetNS))
	if len(netnsref) == 0 {
		reason := "could not locate network namespace for container"
		conn.Errorf(reason)
		conn.GracefullyClose(websocket.CloseAbnormalClosure, reason)
		return
	}
	conn.Debugf("capturing from %s network interfaces: %s",
		target.Type, strings.Join(target.NetworkInterfaces, ", "))
	if len(args.CaptureFilter) > 0 {
		conn.Debugf("filtering capture: %s", args.CaptureFilter)
	}
	conn.Debugf("referencing netns:[%d] as \"%s\"", target.NetNS, netnsref)

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
		conn.Errorf(fmt.Sprintf("cannot not lock netns:[%d], reason: %s", target.NetNS, err.Error()))
		conn.GracefullyClose(websocket.CloseAbnormalClosure, "cannot lock target network stack")
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
		"-n",
		// keep (almost) quiet: no packet capture count, but still some initial messages :(
		"-q",
	}
	// As we want to apply "avoid promiscuous mode" to all network interfaces, we need
	// to specify it *before* the list of network interfaces.
	if args.KeepChaste {
		conn.Debugf("avoiding promiscuous mode, if possible, on all network interfaces")
		captargs = append(captargs, "-p")
	}
	for _, nif := range target.NetworkInterfaces {
		captargs = append(captargs, "-i", nif)
	}
	if len(args.CaptureFilter) > 0 {
		conn.Debugf("capture filter: %s", args.CaptureFilter)
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
	conn.Debugf("starting capture command...")
	res, err := ops.Execute(
		func() interface{} {
			// This will be executed in a separate OS-locked Go routine,
			// switched into the target's network namespace. dumpcap thus will
			// run attached to the target's network stack. All animal magic
			// thanks to lxkns ;)
			netnsid, _ := ops.NamespacePath(fmt.Sprintf("/proc/%d/ns/net", unix.Gettid())).ID()
			conn.Debugf("running %s inside locked net:[%d]", CaptureProgram, netnsid.Ino)
			// In order to run the unprivileged dumpcap binary with the
			// necessary capabilities, we need to transfer them via the ambient
			// capabilities. If you don't know what ambient capabilities are,
			// then you really don't want to know anyway.
			SetAmbient(caps.CAP_NET_ADMIN, caps.CAP_NET_RAW)
			return cmd.Start()
		},
		lockednetns)
	if err != nil {
		conn.Errorf("cannot switch to target network stack, reason: %s", err.Error)
		conn.InitiateGracefulClose(websocket.CloseAbnormalClosure, "cannot switch to target network stack")
		conn.Watch() // finishes the graceful close.
		return
	}
	err, _ = res.(error)
	if err != nil {
		conn.Errorf("cannot start capture process: %s", err.Error())
		conn.InitiateGracefulClose(websocket.CloseAbnormalClosure, "cannot start capture process")
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
			conn.Errorf("capture process failure: %s", err.Error())
			r := grrr.Reason()
			if len(r) == 0 && !piper.Failed {
				r = "capture process failed"
			}
			if len(r) == 0 {
				conn.InitiateGracefulClose(websocket.CloseNormalClosure, "ciao")
			} else {
				conn.InitiateGracefulClose(websocket.CloseAbnormalClosure, r)
			}
		} else {
			conn.Debugf("capture process terminated successfully")
			conn.InitiateGracefulClose(websocket.CloseNormalClosure, "capture process terminated")
		}
	}()
	// Wait for both the watcher/reader and the process waiter to finish.
	wg.Wait()
}
