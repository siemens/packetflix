// (c) Siemens AG 2023
//
// SPDX-License-Identifier: MIT

package packetflix

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/thediveo/nonstd/xslices"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gleak"
	. "github.com/thediveo/fdooze"
	. "github.com/thediveo/success"
)

func TestPacketflix(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "packetflix package")
}

var _ = BeforeEach(func() {
	DeferCleanup(slog.SetDefault, slog.Default())
	slog.SetDefault(slog.New(slog.NewTextHandler(GinkgoWriter, &slog.HandlerOptions{Level: slog.LevelDebug})))

	goodfds := Filedescriptors()
	goodgos := Goroutines()
	DeferCleanup(func() {
		Eventually(Goroutines).Within(2 * time.Second).ProbeEvery(100 * time.Millisecond).
			ShouldNot(HaveLeaked(goodgos))
		Eventually(Filedescriptors).Within(2 * time.Second).ProbeEvery(100 * time.Millisecond).
			ShouldNot(HaveLeakedFds(goodfds))
	})
})

// newWSPair serves a single websocket connection using the passed serve
// function and returns the connected client side, as well as a channel that
// gets closed after serve has returned.
func newWSPair(serve func(conn *WSConn)) (*websocket.Conn, <-chan struct{}) {
	GinkgoHelper()
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		defer close(done)
		defer GinkgoRecover()
		conn := NewWSConn()
		conn.Conn = Successful((&websocket.Upgrader{}).Upgrade(w, req, nil))
		defer func() { _ = conn.Close() }()
		serve(conn)
	}))
	DeferCleanup(srv.Close)
	client, _ := Successful2R(websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil))
	DeferCleanup(func() { _ = client.Close() })
	return client, done
}

// fakeDiscovery runs a discovery service test server with the passed handler
// until the end of the current spec, returning its "host:port" address.
func fakeDiscovery(handler http.HandlerFunc) string {
	GinkgoHelper()
	srv := httptest.NewServer(handler)
	DeferCleanup(srv.Close)
	// The discovery clients use the default transport and its idle connections.
	DeferCleanup(AssignableTo[*http.Transport](http.DefaultTransport).CloseIdleConnections)
	return srv.Listener.Addr().String()
}

// startTime returns the start time of the specified process in clock ticks
// since boot.
func startTime(pid int) int64 {
	GinkgoHelper()
	stat := string(Successful(os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))))
	fields := strings.Fields(stat[strings.LastIndex(stat, ")")+2:])
	return Successful(strconv.ParseInt(fields[22-3], 10, 64))
}

// frame is a websocket frame received from the server.
type frame struct {
	opcode int
	at     time.Time
}

// readFrames returns the frames the server sends on the passed raw connection
// until the connection fails or the deadline passes. It supports only frames
// with payloads shorter than 126 octets, such as control frames.
func readFrames(conn net.Conn, deadline time.Time) []frame {
	GinkgoHelper()
	Expect(conn.SetReadDeadline(deadline)).To(Succeed())
	var frames []frame
	hdr := make([]byte, 2)
	for {
		if _, err := io.ReadFull(conn, hdr); err != nil {
			return frames
		}
		if _, err := io.ReadFull(conn, make([]byte, hdr[1]&0x7f)); err != nil {
			return frames
		}
		frames = append(frames, frame{opcode: int(hdr[0] & 0x0f), at: time.Now()})
	}
}

// opcodes returns the opcodes of the passed frames.
func opcodes(frames []frame) []int {
	return xslices.Map(frames, func(f frame) int { return f.opcode })
}
