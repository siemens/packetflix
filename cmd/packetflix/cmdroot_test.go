// (c) Siemens AG 2026
//
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"

	"github.com/thediveo/clippy/debug"
	"github.com/thediveo/safe"

	"github.com/siemens/packetflix"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/thediveo/success"
)

var _ = Describe("packetflix command", func() {

	It("shows help with all flags", func() {
		var out safe.Buffer
		rootCmd := newRootCmd()
		rootCmd.SetArgs([]string{"--help"})
		rootCmd.SetOut(io.MultiWriter(&out, GinkgoWriter))
		Expect(rootCmd.Execute()).To(Succeed())
		Expect(out.String()).To(SatisfyAll(
			MatchRegexp(`-p, --port uint16 .*\(default 5001\)`),
			ContainSubstring("--discovery-service string"),
			ContainSubstring("--gw-port uint16"),
			ContainSubstring("--proxy-discovery"),
			ContainSubstring("--log-requests"),
			ContainSubstring("--log-headers"),
			ContainSubstring("--debug"),
			ContainSubstring("--version"),
		))
	})

	It("shows its version", func() {
		var out safe.Buffer
		rootCmd := newRootCmd()
		rootCmd.SetArgs([]string{"--version"})
		rootCmd.SetOut(io.MultiWriter(&out, GinkgoWriter))
		Expect(rootCmd.Execute()).To(Succeed())
		Expect(out.String()).To(Equal("packetflix version " + buildVersion() + "\n"))
	})

	It("rejects arguments", func() {
		rootCmd := newRootCmd()
		rootCmd.SetArgs([]string{"foo"})
		rootCmd.SetOut(GinkgoWriter)
		rootCmd.SetErr(GinkgoWriter)
		Expect(rootCmd.Execute()).To(MatchError(ContainSubstring(`unknown command "foo"`)))
	})

	It("fails when it cannot listen on its port", func() {
		occupied := Successful(net.Listen("tcp", ":0"))
		DeferCleanup(occupied.Close)
		rootCmd := newRootCmd()
		rootCmd.SetContext(context.Background())
		debug.SetWriter(rootCmd, GinkgoWriter)
		rootCmd.SetArgs([]string{"--port", strconv.Itoa(occupied.Addr().(*net.TCPAddr).Port)})
		rootCmd.SetOut(GinkgoWriter)
		rootCmd.SetErr(GinkgoWriter)
		Expect(rootCmd.Execute()).To(
			MatchError(ContainSubstring("cannot start capture service")))
	})

	It("serves its API until cancelled", func() {
		baseurl, _ := startService()

		resp := Successful(http.Get(baseurl + "/version"))
		body := Successful(io.ReadAll(resp.Body))
		Expect(resp.Body.Close()).To(Succeed())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		Expect(body).To(MatchJSON(fmt.Sprintf(`{"name":"packetflix","version":%q}`, buildVersion())))

		resp = Successful(http.Get(baseurl + "/"))
		Expect(resp.Body.Close()).To(Succeed())
		Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
	})

	It("logs requests when asked to", func() {
		baseurl, logs := startService("--log-requests")
		resp := Successful(http.Get(baseurl + "/version"))
		Expect(resp.Body.Close()).To(Succeed())
		Eventually(logs.String).Should(MatchRegexp(`msg=request .* path=/version `))
	})

	It("proxies to the discovery service when asked to", func() {
		ghostwire := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if _, ok := req.Header[packetflix.CaptureEnableHeader]; !ok || req.URL.Path != "/foo" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		}))
		DeferCleanup(ghostwire.Close)
		ghostwireAddr := ghostwire.Listener.Addr().(*net.TCPAddr)

		baseurl, _ := startService("--proxy-discovery",
			"--discovery-service", ghostwireAddr.IP.String(),
			"--gw-port", strconv.Itoa(ghostwireAddr.Port))
		resp := Successful(http.Get(baseurl + "/foo"))
		Expect(resp.Body.Close()).To(Succeed())
		Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
	})

})
