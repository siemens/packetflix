// (c) Siemens AG 2026
//
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"testing"
	"time"

	"github.com/thediveo/clippy/debug"
	"github.com/thediveo/safe"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gleak"
	. "github.com/thediveo/fdooze"
	. "github.com/thediveo/success"
)

func TestCmdPacketflix(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "packetflix cmd/packetflix package")
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

// startService runs the packetflix root command with the passed CLI args on a
// free port until the end of the current spec, returning the base URL of the
// service as well as its logs.
func startService(args ...string) (string, *safe.Buffer) {
	GinkgoHelper()
	// Requests to the service use the default transport and its idle connections.
	DeferCleanup(AssignableTo[*http.Transport](http.DefaultTransport).CloseIdleConnections)

	var logs safe.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	rootCmd := newRootCmd()
	// SetWriter attaches to the command's context, so set ours first.
	rootCmd.SetContext(ctx)
	debug.SetWriter(rootCmd, io.MultiWriter(&logs, GinkgoWriter))
	rootCmd.SetOut(GinkgoWriter)
	rootCmd.SetErr(GinkgoWriter)
	rootCmd.SetArgs(append([]string{"--port", "0"}, args...))

	done := make(chan error, 1)
	go func() { done <- rootCmd.Execute() }()
	DeferCleanup(func() {
		cancel()
		Eventually(done).Should(Receive(BeNil()))
	})

	addrRe := regexp.MustCompile(`addr=\S*:(\d+)`)
	Eventually(logs.String).Should(MatchRegexp(addrRe.String()))
	return "http://127.0.0.1:" + addrRe.FindStringSubmatch(logs.String())[1], &logs
}
