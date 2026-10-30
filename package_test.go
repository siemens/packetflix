// (c) Siemens AG 2023
//
// SPDX-License-Identifier: MIT

package main

import (
	"log/slog"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gleak"
	. "github.com/thediveo/fdooze"
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
