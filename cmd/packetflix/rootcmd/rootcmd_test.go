// (c) Siemens AG 2026
//
// SPDX-License-Identifier: MIT

package rootcmd

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"

	"github.com/spf13/cobra"
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
		rootCmd := New(Variant{})
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
		rootCmd := New(Variant{})
		rootCmd.SetArgs([]string{"--version"})
		rootCmd.SetOut(io.MultiWriter(&out, GinkgoWriter))
		Expect(rootCmd.Execute()).To(Succeed())
		Expect(out.String()).To(Equal("packetflix version " + buildVersion("") + "\n"))
	})

	It("rejects arguments", func() {
		rootCmd := New(Variant{})
		rootCmd.SetArgs([]string{"foo"})
		rootCmd.SetOut(GinkgoWriter)
		rootCmd.SetErr(GinkgoWriter)
		Expect(rootCmd.Execute()).To(MatchError(ContainSubstring(`unknown command "foo"`)))
	})

	It("fails when it cannot listen on its port", func() {
		occupied := Successful(net.Listen("tcp", ":0"))
		DeferCleanup(occupied.Close)
		rootCmd := New(Variant{})
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
		Expect(body).To(MatchJSON(fmt.Sprintf(`{"name":"packetflix","version":%q}`, buildVersion(""))))

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

	It("executes a command", func() {
		var out safe.Buffer
		rootCmd := New(Variant{})
		rootCmd.SetArgs([]string{"--version"})
		rootCmd.SetOut(io.MultiWriter(&out, GinkgoWriter))
		Expect(Execute(rootCmd)).To(Succeed())
		Expect(out.String()).To(ContainSubstring(buildVersion("")))
	})

	When("assembling a variant", func() {

		It("uses its own name and version", func() {
			var out safe.Buffer
			rootCmd := New(Variant{Name: "variantflix", Version: "v6.6.6"})
			rootCmd.SetArgs([]string{"--version"})
			rootCmd.SetOut(io.MultiWriter(&out, GinkgoWriter))
			Expect(rootCmd.Execute()).To(Succeed())
			Expect(out.String()).To(Equal("variantflix version 6.6.6\n"))
		})

		It("reports its own name and version", func() {
			baseurl, _ := startVariant(Variant{Name: "variantflix", Version: "6.6.6"})
			resp := Successful(http.Get(baseurl + "/version"))
			body := Successful(io.ReadAll(resp.Body))
			Expect(resp.Body.Close()).To(Succeed())
			Expect(body).To(MatchJSON(`{"name":"variantflix","version":"6.6.6"}`))
		})

		It("serves on multiple servers, also with TLS", func() {
			// borrow the TLS configuration and the matching client.
			tlsServer := httptest.NewTLSServer(http.NotFoundHandler())
			DeferCleanup(tlsServer.Close)
			tlsAddr := Successful(net.Listen("tcp", "127.0.0.1:0"))
			addr := tlsAddr.Addr().String()
			Expect(tlsAddr.Close()).To(Succeed())

			baseurl, logs := startVariant(Variant{
				Servers: func(cmd *cobra.Command) ([]*http.Server, error) {
					teapot := http.NewServeMux()
					teapot.HandleFunc("/teapot", func(w http.ResponseWriter, _ *http.Request) {
						w.WriteHeader(http.StatusTeapot)
					})
					return []*http.Server{
						{Addr: ":0", Handler: LogRequests(cmd, NewMux(cmd))},
						{Addr: addr, Handler: teapot, TLSConfig: tlsServer.TLS.Clone()},
					}, nil
				},
			})
			Eventually(logs.String).Should(MatchRegexp(`(?s)addr=.*addr=`))

			resp := Successful(tlsServer.Client().Get("https://" + addr + "/teapot"))
			Expect(resp.Body.Close()).To(Succeed())
			Expect(resp.StatusCode).To(Equal(http.StatusTeapot))
			Expect(resp.TLS).NotTo(BeNil())

			resp = Successful(http.Get(baseurl + "/version"))
			Expect(resp.Body.Close()).To(Succeed())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})

		// failVariant runs the root command of the passed variant, expecting
		// it to fail and returning its error.
		failVariant := func(v Variant) error {
			GinkgoHelper()
			rootCmd := New(v)
			rootCmd.SetContext(context.Background())
			debug.SetWriter(rootCmd, GinkgoWriter)
			rootCmd.SetOut(GinkgoWriter)
			rootCmd.SetErr(GinkgoWriter)
			rootCmd.SetArgs([]string{})
			err := rootCmd.Execute()
			Expect(err).To(HaveOccurred())
			return err
		}

		It("fails when it cannot create its servers", func() {
			Expect(failVariant(Variant{
				Servers: func(*cobra.Command) ([]*http.Server, error) { return nil, errors.New("D'OH!") },
			})).To(MatchError("cannot start capture service: D'OH!"))
		})

		It("fails without any servers", func() {
			Expect(failVariant(Variant{
				Servers: func(*cobra.Command) ([]*http.Server, error) { return nil, nil },
			})).To(MatchError("cannot start capture service: no servers"))
		})

		It("closes earlier listeners when a later server cannot listen", func() {
			occupied := Successful(net.Listen("tcp", "127.0.0.1:0"))
			DeferCleanup(occupied.Close)
			Expect(failVariant(Variant{
				Servers: func(*cobra.Command) ([]*http.Server, error) {
					return []*http.Server{{Addr: "127.0.0.1:0"}, {Addr: occupied.Addr().String()}}, nil
				},
			})).To(MatchError(ContainSubstring("address already in use")))
		})

		It("stops all servers when one fails", func() {
			Expect(failVariant(Variant{
				Servers: func(*cobra.Command) ([]*http.Server, error) {
					return []*http.Server{
						{Addr: "127.0.0.1:0"},
						// without certificates, ServeTLS tries to load them from "".
						{Addr: "127.0.0.1:0", TLSConfig: &tls.Config{}},
					}, nil
				},
			})).To(MatchError(fs.ErrNotExist))
		})

	})

})
