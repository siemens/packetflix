// (c) Siemens AG 2026
//
// SPDX-License-Identifier: MIT

package downstream_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/siemens/csharg/v2/api"
	"github.com/thediveo/clippy/debug"
	"github.com/thediveo/safe"
	"github.com/thediveo/spacetest/netns"

	"github.com/siemens/packetflix"
	"github.com/siemens/packetflix/cmd/cli/wsping"
	"github.com/siemens/packetflix/cmd/packetflix/rootcmd"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/thediveo/success"
)

// These specs assemble "variantflix", a downstream packetflix variant, the way
// a separate downstream Go module would: as an external test package, using
// only the exported API of packetflix. The specs thus guard that downstream
// modules can build their own variants without forking packetflix, and break
// as soon as a change to packetflix takes away something a downstream needs.
//
// variantflix stands in for a variant running behind a device's reverse proxy
// that terminates TLS. It brings in:
//
//   - a UI listener serving the UI through the discovery proxy;
//   - a ticket endpoint on the UI listener;
//   - a separate capture listener serving only /capture;
//   - admission of capture requests only when presenting a single-use ticket.
//
// Its stand-in logic – a fixed ticket, an in-memory event log, and a maximum
// capture duration – is just enough to exercise each extension point:
//
//   - own name and version;
//   - own CLI flags;
//   - own routes and middleware per listener;
//   - request headers added towards the discovery service;
//   - admission before the websocket upgrade;
//   - capture arguments supplied by the admission instead of the request;
//   - capture lifecycle events;
//   - stopping captures from the server side;
//   - websocket pings keeping quiet captures alive.
//
// Except for the TLS fallback, the specs reach variantflix only through a
// stand-in reverse proxy that strips route prefixes and passes websocket
// upgrades on, as this is how clients reach it on the devices. Specs running
// real captures need root and skip themselves otherwise.
var _ = Describe("downstream variant", func() {

	var hf *variantflix
	var proxied chan http.Header
	var gwArgs []string

	// pool trusts the server certificate the clients see.
	var pool *x509.CertPool

	BeforeEach(func() {
		registerFlags()

		hf = &variantflix{
			ticket: "open-sesame",
			user:   "alice",
			args: packetflix.Args{
				Target: &api.Target{
					Name:              "chatty",
					NetNS:             int(netns.CurrentIno()),
					NetworkInterfaces: []string{"lo"},
				},
				CaptureFilter: quietFilter,
				KeepChaste:    true,
			},
		}

		proxied = make(chan http.Header, 10)
		ghostwire := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.URL.Path == "/mobyshark" {
				_ = json.NewEncoder(w).Encode(api.GwTargetList{Targets: api.Targets{{
					Name:              "chatty",
					Type:              "docker",
					NetNS:             int(netns.CurrentIno()),
					NetworkInterfaces: []string{"lo", "eth0"},
				}}})
				return
			}
			proxied <- req.Header.Clone()
			w.WriteHeader(http.StatusNoContent)
		}))
		DeferCleanup(ghostwire.Close)
		gwAddr := ghostwire.Listener.Addr().(*net.TCPAddr)
		gwArgs = []string{
			"--proxy-discovery",
			"--discovery-service", gwAddr.IP.String(),
			"--gw-port", strconv.Itoa(gwAddr.Port),
		}
	})

	// behindProxy starts variantflix with the passed CLI args in addition to the
	// discovery flags behind the stand-in reverse proxy, returning the
	// proxied base URLs of the UI and the capture listeners.
	behindProxy := func(args ...string) (uiURL, captureURL string) {
		GinkgoHelper()
		ui, capture := hf.start(append(gwArgs, args...)...)
		var base string
		base, pool = reverseProxy(map[string]string{
			"/edgeshark":  ui,
			"/packetflix": capture,
		})
		return base + "/edgeshark", base + "/packetflix"
	}

	// get returns the response to a GET request for the passed URL.
	get := func(url string) *http.Response {
		GinkgoHelper()
		client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}}
		DeferCleanup(client.CloseIdleConnections)
		resp := Successful(client.Get(url))
		DeferCleanup(resp.Body.Close)
		return resp
	}

	// dial connects to the capture endpoint below the passed (https) base URL
	// with the passed query and headers until the end of the current spec.
	dial := func(captureURL, query string, header http.Header) (*websocket.Conn, *http.Response, error) {
		GinkgoHelper()
		dialer := websocket.Dialer{
			TLSClientConfig:  &tls.Config{RootCAs: pool},
			HandshakeTimeout: 5 * time.Second,
		}
		client, resp, err := dialer.Dial("wss"+strings.TrimPrefix(captureURL, "https")+"/capture"+query, header)
		if resp != nil {
			DeferCleanup(resp.Body.Close)
		}
		if client != nil {
			DeferCleanup(func() { _ = client.Close() })
			Expect(client.SetReadDeadline(time.Now().Add(15 * time.Second))).To(Succeed())
		}
		return client, resp, err
	}

	// ticketHeader returns the header presenting the ticket.
	ticketHeader := func() http.Header {
		header := http.Header{}
		header.Set("Authorization", "Bearer "+hf.ticket)
		return header
	}

	// conflictingRequest returns the query and headers presenting the ticket,
	// both with capture parameters conflicting with the ticket's.
	conflictingRequest := func() (string, http.Header) {
		header := ticketHeader()
		header.Set("Clustershark-Nif", "nosuchnif")
		header.Set("Clustershark-Filter", "not(valid")
		return "?netns=1&nif=nosuchnif&filter=" + url.QueryEscape("not(valid"), header
	}

	// received is what a capture client receives until the connection closes.
	type received struct {
		data  []time.Time // arrival times of data messages.
		pings []time.Time // arrival times of pings.
		err   error       // final error.
	}

	// receive reads from the passed capture client until the connection
	// closes, answering pings.
	receive := func(client *websocket.Conn) received {
		var r received
		var mu sync.Mutex
		client.SetPingHandler(func(data string) error {
			mu.Lock()
			r.pings = append(r.pings, time.Now())
			mu.Unlock()
			return client.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(time.Second))
		})
		for {
			_, _, err := client.ReadMessage()
			mu.Lock()
			if err != nil {
				r.err = err
				mu.Unlock()
				return r
			}
			r.data = append(r.data, time.Now())
			mu.Unlock()
		}
	}

	// finalEvent waits for the final lifecycle event of the single capture.
	finalEvent := func() packetflix.CaptureEvent {
		GinkgoHelper()
		Eventually(hf.Events).Within(15 * time.Second).Should(ContainElement(
			HaveField("Type", BeElementOf(packetflix.CaptureFailed, packetflix.CaptureEnded))))
		events := hf.Events()
		return events[len(events)-1]
	}

	needsRoot := func() {
		if os.Geteuid() != 0 {
			Skip("needs root")
		}
	}

	It("has its own name, version, and CLI flags", func() {
		var out safe.Buffer
		cmd := rootcmd.New(hf.variant())
		cmd.SetArgs([]string{"--help"})
		cmd.SetOut(io.MultiWriter(&out, GinkgoWriter))
		Expect(cmd.Execute()).To(Succeed())
		Expect(out.String()).To(SatisfyAll(
			ContainSubstring("variantflix [flags]"),
			ContainSubstring("--"+maxDurationFlag+" duration"),
			ContainSubstring("--"+wsping.IntervalFlag+" duration"),
			ContainSubstring("--port"),
			ContainSubstring("--proxy-discovery"),
			ContainSubstring("--log-requests")))

		uiURL, _ := behindProxy()
		resp := get(uiURL + "/version")
		Expect(io.ReadAll(resp.Body)).To(MatchJSON(`{"name":"variantflix","version":"6.6.6"}`))
	})

	It("serves its own routes and middleware on the UI listener, and only /capture on the capture listener", func() {
		uiURL, captureURL := behindProxy()

		resp := get(uiURL + "/ticket")
		Expect(io.ReadAll(resp.Body)).To(BeEquivalentTo(hf.ticket))
		Expect(resp.Header.Get("Cache-Control")).To(Equal("no-store"))

		Expect(get(uiURL + "/capture").StatusCode).To(Equal(http.StatusNotFound))
		Expect(get(captureURL + "/version").StatusCode).To(Equal(http.StatusNotFound))
		Expect(get(captureURL + "/ticket").StatusCode).To(Equal(http.StatusNotFound))
	})

	It("adds request headers when proxying to the discovery service", func() {
		uiURL, _ := behindProxy()
		Expect(get(uiURL + "/ui").StatusCode).To(Equal(http.StatusNoContent))
		var header http.Header
		Expect(proxied).To(Receive(&header))
		Expect(header.Get("X-Variantflix-User")).To(Equal("alice"))
		Expect(header).To(HaveKey(packetflix.CaptureEnableHeader))
	})

	It("refuses capture requests without a valid ticket before upgrading", func() {
		_, captureURL := behindProxy()

		for _, auth := range []string{"", "Bearer wrong"} {
			header := http.Header{}
			if auth != "" {
				header.Set("Authorization", auth)
			}
			_, resp, err := dial(captureURL, "", header)
			Expect(err).To(MatchError(websocket.ErrBadHandshake))
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
			Expect(resp.Header.Get("Upgrade")).To(BeEmpty())
		}
		// Not even trying to upgrade, but with "Connection: upgrade" set by the
		// reverse proxy anyway.
		Expect(get(captureURL + "/capture").StatusCode).To(Equal(http.StatusUnauthorized))
		Expect(hf.Refused()).To(Equal(3))
		Expect(hf.Events()).To(BeEmpty())
	})

	It("consumes the ticket", func() {
		_, captureURL := behindProxy("--"+maxDurationFlag, "500ms")
		client, _, err := dial(captureURL, "", ticketHeader())
		Expect(err).NotTo(HaveOccurred())
		Expect(receive(client).err).To(HaveOccurred())
		_ = finalEvent()

		_, resp, err := dial(captureURL, "", ticketHeader())
		Expect(err).To(MatchError(websocket.ErrBadHandshake))
		Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
	})

	It("refreshes a stale target supplied by the admission", func() {
		// The PID with a wrong start time makes the target stale.
		hf.args.NetNS = 1
		hf.args.Pid = os.Getpid()
		hf.args.StartTime = 1
		_, captureURL := behindProxy("--"+maxDurationFlag, "500ms")
		client, _, err := dial(captureURL, "", ticketHeader())
		Expect(err).NotTo(HaveOccurred())
		Expect(receive(client).err).To(HaveOccurred())

		ev := finalEvent()
		Expect(ev.Args).NotTo(BeNil())
		Expect(ev.Args.Target).To(SatisfyAll(
			HaveField("NetNS", int(netns.CurrentIno())),
			HaveField("Type", "docker"),
			HaveField("NetworkInterfaces", []string{"lo"})))
		Expect(hf.args.NetNS).To(Equal(1), "supplied arguments must stay unmodified")
	})

	It("captures with the admitted arguments only, failing without privileges", func() {
		if os.Geteuid() == 0 {
			Skip("needs non-root")
		}
		_, captureURL := behindProxy()
		query, header := conflictingRequest()
		client, _, err := dial(captureURL, query, header)
		Expect(err).NotTo(HaveOccurred())

		Expect(receive(client).err).To(SatisfyAll(
			BeAssignableToTypeOf(&websocket.CloseError{}),
			HaveField("Code", websocket.CloseInternalServerErr)))

		ev := finalEvent()
		Expect(hf.Events()).To(HaveLen(1))
		Expect(ev).To(SatisfyAll(
			HaveField("Type", packetflix.CaptureFailed),
			HaveField("Request.Header", HaveKey("Authorization")),
			HaveField("ConnID", Not(BeEmpty())),
			HaveField("Code", websocket.CloseInternalServerErr),
			HaveField("Reason", BeElementOf(
				"cannot lock target network stack",
				"cannot switch to target network stack")),
			HaveField("Args.CaptureFilter", quietFilter),
			HaveField("Args.KeepChaste", true),
			HaveField("Args.Target.NetNS", int(netns.CurrentIno())),
			HaveField("Args.Target.NetworkInterfaces", []string{"lo"})))
	})

	It("captures with the admitted arguments only and stops after the maximum duration", func() {
		needsRoot()
		_, captureURL := behindProxy("--"+maxDurationFlag, "2s")
		query, header := conflictingRequest()
		client, _, err := dial(captureURL, query, header)
		Expect(err).NotTo(HaveOccurred())

		// Any of the conflicting parameters would fail the capture.
		started := time.Now()
		r := receive(client)
		Expect(r.err).To(MatchError(&websocket.CloseError{
			Code: websocket.ClosePolicyViolation,
			Text: stoppedReason,
		}))
		Expect(time.Since(started)).To(BeNumerically(">=", 1500*time.Millisecond))
		Expect(r.data).NotTo(BeEmpty(), "pcapng stream")

		ev := finalEvent()
		events := hf.Events()
		Expect(events).To(HaveLen(2))
		Expect(events[0]).To(SatisfyAll(
			HaveField("Type", packetflix.CaptureStarted),
			HaveField("Request.URL.Path", "/capture"),
			HaveField("Args.CaptureFilter", quietFilter),
			HaveField("Args.KeepChaste", true),
			HaveField("Args.Target.NetworkInterfaces", []string{"lo"})))
		Expect(ev).To(SatisfyAll(
			HaveField("Type", packetflix.CaptureEnded),
			HaveField("ConnID", events[0].ConnID),
			HaveField("Code", websocket.ClosePolicyViolation),
			HaveField("Reason", stoppedReason)))
		// Stopping an ended capture does nothing.
		events[0].Stop(websocket.CloseNormalClosure, "too late")
	})

	It("keeps quiet captures alive with pings", func() {
		needsRoot()
		const interval = 250 * time.Millisecond
		_, captureURL := behindProxy("--"+maxDurationFlag, "2500ms", "--"+wsping.IntervalFlag, interval.String())
		client, _, err := dial(captureURL, "", ticketHeader())
		Expect(err).NotTo(HaveOccurred())

		r := receive(client)
		Expect(r.err).To(MatchError(&websocket.CloseError{
			Code: websocket.ClosePolicyViolation,
			Text: stoppedReason,
		}))
		Expect(r.data).NotTo(BeEmpty(), "pcapng stream")
		lastData := r.data[len(r.data)-1]
		quietPings := 0
		for _, ping := range r.pings {
			if ping.After(lastData) {
				quietPings++
			}
		}
		Expect(quietPings).To(BeNumerically(">=", 5), "pings while capturing no data")
		Expect(r.pings[len(r.pings)-1].Sub(r.pings[0]) / time.Duration(len(r.pings)-1)).
			To(BeNumerically("~", interval, 100*time.Millisecond))
	})

	It("doesn't ping when switched off", func() {
		needsRoot()
		_, captureURL := behindProxy("--"+maxDurationFlag, "1500ms", "--"+wsping.IntervalFlag, "0")
		client, _, err := dial(captureURL, "", ticketHeader())
		Expect(err).NotTo(HaveOccurred())

		r := receive(client)
		Expect(r.err).To(MatchError(&websocket.CloseError{
			Code: websocket.ClosePolicyViolation,
			Text: stoppedReason,
		}))
		Expect(r.data).NotTo(BeEmpty(), "pcapng stream")
		Expect(r.pings).To(BeEmpty())
	})

	It("stops a capture even when the client doesn't acknowledge", func() {
		needsRoot()
		_, captureURL := behindProxy("--"+maxDurationFlag, "500ms")
		_, _, err := dial(captureURL, "", ticketHeader())
		Expect(err).NotTo(HaveOccurred())
		// Never read, so never acknowledge the server's close.
		Eventually(hf.Events).Within(2 * time.Second).Should(ContainElement(
			HaveField("Type", packetflix.CaptureStarted)))
		Eventually(hf.Events).Within(packetflix.ClosingDeadline + 5*time.Second).Should(ContainElement(SatisfyAll(
			HaveField("Type", packetflix.CaptureEnded),
			HaveField("Reason", stoppedReason))))
	})

	It("shuts down all listeners together", func() {
		var logs safe.Buffer
		ctx, cancel := context.WithCancel(context.Background())
		cmd := rootcmd.New(hf.variant())
		cmd.SetContext(ctx)
		debug.SetWriter(cmd, io.MultiWriter(&logs, GinkgoWriter))
		cmd.SetOut(GinkgoWriter)
		cmd.SetErr(GinkgoWriter)
		cmd.SetArgs(gwArgs)
		done := make(chan error, 1)
		go func() { done <- cmd.Execute() }()
		Eventually(logs.String).Should(MatchRegexp(`(?s)addr=.*addr=`))
		cancel()
		Eventually(done).Within(5 * time.Second).Should(Receive(BeNil()))
		Expect(logs.String()).To(ContainSubstring(fmt.Sprintf("msg=%q", "capture service stopped")))
	})

	When("falling back to TLS on the capture listener", func() {

		BeforeEach(func() {
			hf.tlsFallback = true
			hf.cert, pool = selfSigned()
		})

		It("serves only /capture with TLS", func() {
			_, captureURL := hf.start(gwArgs...)

			resp := get(captureURL + "/version")
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			Expect(resp.TLS).NotTo(BeNil())

			plain := Successful(http.Get("http" + strings.TrimPrefix(captureURL, "https") + "/capture"))
			Expect(plain.Body.Close()).To(Succeed())
			Expect(plain.StatusCode).To(Equal(http.StatusBadRequest), "plain HTTP to the TLS listener")
		})

		It("refuses capture requests without a valid ticket before upgrading", func() {
			_, captureURL := hf.start(gwArgs...)
			_, resp, err := dial(captureURL, "", nil)
			Expect(err).To(MatchError(websocket.ErrBadHandshake))
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
			Expect(resp.Header.Get("Upgrade")).To(BeEmpty())
		})

		It("captures with a valid ticket", func() {
			_, captureURL := hf.start(append(gwArgs, "--"+maxDurationFlag, "500ms")...)
			client, _, err := dial(captureURL, "", ticketHeader())
			Expect(err).NotTo(HaveOccurred())
			Expect(receive(client).err).To(BeAssignableToTypeOf(&websocket.CloseError{}))
			Expect(finalEvent()).To(HaveField("Request.TLS", Not(BeNil())))
		})

	})

})
