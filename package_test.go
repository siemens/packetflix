// (c) Siemens AG 2023
//
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

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
		conn.Conn = Successful(wsupgrader.Upgrade(w, req, nil))
		defer func() { _ = conn.Close() }()
		serve(conn)
	}))
	DeferCleanup(srv.Close)
	client, _ := Successful2R(websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil))
	DeferCleanup(func() { _ = client.Close() })
	return client, done
}

// fakeDiscovery points the discovery service settings to a test server with
// the passed handler, restoring the original settings at cleanup.
func fakeDiscovery(handler http.HandlerFunc) {
	GinkgoHelper()
	srv := httptest.NewServer(handler)
	DeferCleanup(srv.Close)
	// The discovery clients use the default transport and its idle connections.
	DeferCleanup(AssignableTo[*http.Transport](http.DefaultTransport).CloseIdleConnections)
	srvurl := Successful(url.Parse(srv.URL))
	oldservice, oldport := DiscoveryService, DiscoveryPort
	DeferCleanup(func() { DiscoveryService, DiscoveryPort = oldservice, oldport })
	DiscoveryService = srvurl.Hostname()
	DiscoveryPort = uint16(Successful(strconv.ParseUint(srvurl.Port(), 10, 16)))
}

// startTime returns the start time of the specified process in clock ticks
// since boot.
func startTime(pid int) int64 {
	GinkgoHelper()
	stat := string(Successful(os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))))
	fields := strings.Fields(stat[strings.LastIndex(stat, ")")+2:])
	return Successful(strconv.ParseInt(fields[22-3], 10, 64))
}
