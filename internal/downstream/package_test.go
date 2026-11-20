// (c) Siemens AG 2026
//
// SPDX-License-Identifier: MIT

package downstream_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
	"github.com/thediveo/certifake"
	"github.com/thediveo/clippy/cliplugin"
	"github.com/thediveo/clippy/debug"
	"github.com/thediveo/go-plugger/v3"
	"github.com/thediveo/safe"

	"github.com/siemens/packetflix"
	"github.com/siemens/packetflix/cmd/cli/discovery"
	"github.com/siemens/packetflix/cmd/cli/wsping"
	"github.com/siemens/packetflix/cmd/packetflix/rootcmd"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gleak"
	. "github.com/thediveo/fdooze"
	. "github.com/thediveo/success"
)

func TestDownstream(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "packetflix internal/downstream package")
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

// maxDurationFlag is the CLI flag a downstream plugin adds.
const maxDurationFlag = "max-capture-duration"

// quietFilter is a capture filter that lets no packets pass in the test
// environment, so captures stay quiet after the initial pcapng blocks.
const quietFilter = "udp port 9"

// stoppedReason is the close reason when the server stops a capture.
const stoppedReason = "maximum capture duration reached"

// registerFlags registers a downstream CLI flag plugin until the end of the
// current spec, as a downstream plugin package would do in its init function.
func registerFlags() {
	DeferCleanup(plugger.Group[cliplugin.SetupCLI]().Restore, plugger.Group[cliplugin.SetupCLI]().Backup())
	plugger.Group[cliplugin.SetupCLI]().Register(func(cmd *cobra.Command) {
		cmd.PersistentFlags().Duration(maxDurationFlag, time.Hour, "maximum duration of a capture")
	}, plugger.WithPlugin("variantflix"))
}

// variantflix is a stand-in for a downstream packetflix variant behind a
// reverse proxy: a plain-HTTP UI listener serves the UI and discovery proxy as
// well as a ticket endpoint, and a separate plain-HTTP capture listener serves
// only /capture, admitting only requests presenting the single-use ticket. As
// a fallback, the capture listener serves TLS itself.
type variantflix struct {
	ticket      string          // fixed stand-in for a real ticket.
	args        packetflix.Args // capture arguments bound to the ticket.
	user        string          // stand-in for the authenticated browser user.
	tlsFallback bool            // capture listener serves TLS itself.
	cert        *tls.Certificate
	mu          sync.Mutex
	used        bool
	refused     int
	events      []packetflix.CaptureEvent
}

// variant returns the downstream service variant.
func (h *variantflix) variant() rootcmd.Variant {
	return rootcmd.Variant{
		Name:    "variantflix",
		Version: "v6.6.6",
		Servers: func(cmd *cobra.Command) ([]*http.Server, error) {
			maxDuration, err := cmd.Flags().GetDuration(maxDurationFlag)
			if err != nil {
				return nil, err
			}
			gw := discovery.Address(cmd)

			proxy := packetflix.NewDiscoveryProxy(gw)
			rewrite := proxy.Rewrite
			proxy.Rewrite = func(pr *httputil.ProxyRequest) {
				rewrite(pr)
				pr.Out.Header.Set("X-Variantflix-User", h.user)
			}
			internal := http.NewServeMux()
			internal.Handle("/version", rootcmd.NewVersionHandler(cmd))
			internal.Handle("/capture", http.NotFoundHandler())
			internal.HandleFunc("/ticket", func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, h.ticket)
			})
			internal.Handle("/", proxy)

			capture := http.NewServeMux()
			capture.Handle("/capture", packetflix.NewCaptureHandlerWith(gw,
				packetflix.WithAdmission(h.admit),
				packetflix.WithObserver(func(ev packetflix.CaptureEvent) {
					h.observe(ev, maxDuration)
				}),
				packetflix.WithPingInterval(wsping.Interval(cmd))))
			captureServer := &http.Server{
				Addr:              "127.0.0.1:0",
				Handler:           rootcmd.LogRequests(cmd, capture),
				ReadHeaderTimeout: 5 * time.Second,
			}
			if h.tlsFallback {
				captureServer.TLSConfig = &tls.Config{
					// allows replacing the certificate at runtime.
					GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
						return h.cert, nil
					},
				}
			}

			return []*http.Server{
				{
					Addr:              "127.0.0.1:0",
					Handler:           rootcmd.LogRequests(cmd, noStore(internal)),
					ReadHeaderTimeout: 5 * time.Second,
				},
				captureServer,
			}, nil
		},
	}
}

// noStore is a downstream middleware.
func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, req)
	})
}

// admit admits only requests presenting the unused ticket, returning the
// capture arguments bound to the ticket.
func (h *variantflix) admit(w http.ResponseWriter, req *http.Request) (*packetflix.Args, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.used || req.Header.Get("Authorization") != "Bearer "+h.ticket {
		h.refused++
		http.Error(w, "no valid ticket", http.StatusUnauthorized)
		return nil, false
	}
	h.used = true
	args := h.args
	target := *h.args.Target
	args.Target = &target
	return &args, true
}

// observe records the capture lifecycle events and stops captures after the
// maximum duration.
func (h *variantflix) observe(ev packetflix.CaptureEvent, maxDuration time.Duration) {
	h.mu.Lock()
	h.events = append(h.events, ev)
	h.mu.Unlock()
	if ev.Type == packetflix.CaptureStarted {
		time.AfterFunc(maxDuration, func() {
			ev.Stop(websocket.ClosePolicyViolation, stoppedReason)
		})
	}
}

// Events returns the capture lifecycle events recorded so far.
func (h *variantflix) Events() []packetflix.CaptureEvent {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.events)
}

// Refused returns the number of refused capture requests so far.
func (h *variantflix) Refused() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.refused
}

// start runs the variantflix root command with the passed CLI args until the
// end of the current spec, returning the base URLs of the UI and the capture
// listeners.
func (h *variantflix) start(args ...string) (uiURL, captureURL string) {
	GinkgoHelper()
	DeferCleanup(AssignableTo[*http.Transport](http.DefaultTransport).CloseIdleConnections)

	var logs safe.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	rootCmd := rootcmd.New(h.variant())
	rootCmd.SetContext(ctx)
	debug.SetWriter(rootCmd, io.MultiWriter(&logs, GinkgoWriter))
	rootCmd.SetOut(GinkgoWriter)
	rootCmd.SetErr(GinkgoWriter)
	rootCmd.SetArgs(args)

	done := make(chan error, 1)
	go func() { done <- rootCmd.Execute() }()
	DeferCleanup(func() {
		cancel()
		Eventually(done).Within(5 * time.Second).Should(Receive(BeNil()))
	})

	addrRe := regexp.MustCompile(`addr=(\S+)`)
	Eventually(func() int { return len(addrRe.FindAllString(logs.String(), -1)) }).Should(Equal(2))
	addrs := addrRe.FindAllStringSubmatch(logs.String(), -1)
	captureURL = "http://" + addrs[1][1]
	if h.tlsFallback {
		captureURL = "https://" + addrs[1][1]
	}
	return "http://" + addrs[0][1], captureURL
}

// reverseProxy runs a stand-in for the device's reverse proxy until the end of
// the current spec, returning its base URL and a pool trusting its
// certificate. Like the device's nginx, the stand-in terminates TLS, forwards
// requests with the passed path prefixes to their upstreams with the prefixes
// stripped, passes websocket upgrades on, and always sets "Connection:
// upgrade".
func reverseProxy(routes map[string]string) (string, *x509.CertPool) {
	GinkgoHelper()
	transport := &http.Transport{}
	DeferCleanup(transport.CloseIdleConnections)
	mux := http.NewServeMux()
	for prefix, upstream := range routes {
		target := Successful(url.Parse(upstream))
		mux.Handle(prefix+"/", &httputil.ReverseProxy{
			Transport: transport,
			ErrorLog:  log.New(GinkgoWriter, "reverse proxy: ", 0),
			Rewrite: func(pr *httputil.ProxyRequest) {
				pr.Out.URL.Path = strings.TrimPrefix(pr.Out.URL.Path, prefix)
				pr.Out.URL.RawPath = ""
				pr.SetURL(target)
				pr.Out.Host = pr.In.Host
				pr.SetXForwarded()
				pr.Out.Header.Set("Connection", "upgrade")
			},
		})
	}
	srv := httptest.NewTLSServer(mux)
	DeferCleanup(srv.Close)
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	return srv.URL, pool
}

// selfSigned returns a new self-signed certificate for 127.0.0.1, as well as a
// certificate pool trusting it.
func selfSigned() (*tls.Certificate, *x509.CertPool) {
	GinkgoHelper()
	cert := Successful(certifake.New(certifake.WithIP("127.0.0.1")))
	pool := x509.NewCertPool()
	pool.AddCert(Successful(x509.ParseCertificate(cert.Certificate[0])))
	return &cert, pool
}
