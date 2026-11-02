// (c) Siemens AG 2023
//
// SPDX-License-Identifier: MIT

package packetflix

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/onsi/gomega/gbytes"
	"github.com/siemens/csharg/v2/api"
	"github.com/thediveo/spacetest/netns"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/thediveo/success"
)

var _ = Describe("capture handler", func() {

	DescribeTable("dumpcap arguments",
		func(args Args, expected []string) {
			Expect(dumpcapArgs(&args)).To(Equal(append([]string{"-w", "-", "-F", "pcapng", "-q"}, expected...)))
		},
		Entry("plain", Args{Target: &api.Target{}}, []string{}),
		Entry("all options",
			Args{
				Target:        &api.Target{NetworkInterfaces: []string{"eth0", "lo"}},
				CaptureFilter: "tcp",
				KeepChaste:    true,
			},
			[]string{"-p", "-f", "tcp", "-i", "eth0", "-i", "lo"}),
	)

	It("refuses plain HTTP requests", func() {
		rec := httptest.NewRecorder()
		NewCaptureHandler("").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/capture", nil))
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
	})

	When("it cannot start capturing", func() {

		var wsurl string

		BeforeEach(func() {
			srv := httptest.NewServer(NewCaptureHandler(""))
			DeferCleanup(srv.Close)
			wsurl = "ws" + strings.TrimPrefix(srv.URL, "http")
		})

		It("closes the websocket on invalid capture parameters", func() {
			client, _ := Successful2R(websocket.DefaultDialer.Dial(wsurl+"/capture", nil))
			DeferCleanup(func() { _ = client.Close() })
			_, _, err := client.ReadMessage()
			Expect(err).To(HaveOccurred())
		})

		It("closes the websocket when the network namespace cannot be located", func() {
			client, _ := Successful2R(websocket.DefaultDialer.Dial(wsurl+"/capture?netns=1", nil))
			DeferCleanup(func() { _ = client.Close() })
			_, _, err := client.ReadMessage()
			Expect(err).To(HaveOccurred())
		})

		It("tells the client the reason", func() {
			client, _ := Successful2R(websocket.DefaultDialer.Dial(wsurl+"/capture", nil))
			DeferCleanup(func() { _ = client.Close() })
			_, _, err := client.ReadMessage()
			Expect(err).To(MatchError(&websocket.CloseError{
				Code: websocket.ClosePolicyViolation,
				Text: "either container or netns query parameter required",
			}))
		})

	})

	When("capturing from the current network namespace", func() {

		var query string

		BeforeEach(func() {
			query = fmt.Sprintf("/capture?netns=%d", netns.CurrentIno())
		})

		It("fails without privileges", func() {
			if os.Geteuid() == 0 {
				Skip("needs non-root")
			}
			wsurl, done := serveCapture(NewCaptureHandler(""))
			_, err := readUntilClosed(dial(wsurl + query + "&nif=lo"))
			Expect(err).To(SatisfyAll(
				BeAssignableToTypeOf(&websocket.CloseError{}),
				HaveField("Code", websocket.CloseInternalServerErr),
				HaveField("Text", BeElementOf(
					"cannot lock target network stack",
					"cannot switch to target network stack"))))
			Eventually(done).Should(BeClosed())
		})

		When("privileged", func() {

			BeforeEach(func() {
				if os.Geteuid() != 0 {
					Skip("needs root")
				}
			})

			It("streams captured packets until the client closes", func() {
				wsurl, done := serveCapture(NewCaptureHandler(""))
				client := dial(wsurl + query + "&nif=lo&filter=udp&chaste")

				marker := []byte("packetflix-test-marker")
				chatter(marker, udpSink())

				var stream []byte
				for !bytes.Contains(stream, marker) {
					_, msg := Successful2R(client.ReadMessage())
					stream = append(stream, msg...)
				}
				Expect(client.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"))).To(Succeed())
				_, err := readUntilClosed(client)
				Expect(err).To(BeAssignableToTypeOf(&websocket.CloseError{}))
				Eventually(done).Within(5 * time.Second).Should(BeClosed())
			})

			It("streams a valid pcapng capture with target information that Wireshark understands", func() {
				tshark := needsPath("tshark")
				capinfos := needsPath("capinfos")

				wanted, unwanted := udpSink(), udpSink()
				wantedPort := wanted.(*net.UDPAddr).Port
				marker := []byte("packetflix-test-marker")
				chatter(marker, wanted, unwanted)

				wsurl, done := serveCapture(NewCaptureHandler(""))
				// Also capture from a second interface, as filters might apply
				// only to the last one (see issue #10).
				target := Successful(json.Marshal(api.Target{
					Name:              "chatty",
					Type:              "docker",
					NetNS:             int(netns.CurrentIno()),
					NetworkInterfaces: []string{"lo", otherInterface()},
				}))
				filter := fmt.Sprintf("udp port %d", wantedPort)
				client := dial(wsurl + "/capture?chaste&container=" + url.QueryEscape(string(target)) +
					"&filter=" + url.QueryEscape(filter))

				packets := gbytes.NewBuffer()
				dissector := exec.Command(tshark, "-r", "-", "-l", "-n",
					"-T", "fields", "-E", "separator=/s",
					"-e", "frame.interface_name", "-e", "ip.dst", "-e", "udp.dstport", "-e", "udp.payload")
				dissector.Stdout = packets
				dissector.Stderr = GinkgoWriter
				dissectorIn := Successful(dissector.StdinPipe())

				infos := gbytes.NewBuffer()
				inspector := exec.Command(capinfos, "-k", "-")
				inspector.Stdout = infos
				inspector.Stderr = GinkgoWriter
				inspectorIn := Successful(inspector.StdinPipe())

				Expect(dissector.Start()).To(Succeed())
				DeferCleanup(func() { _ = dissector.Process.Kill() })
				Expect(inspector.Start()).To(Succeed())
				DeferCleanup(func() { _ = inspector.Process.Kill() })

				pumped := make(chan struct{})
				go func() {
					defer close(pumped)
					defer func() {
						_ = dissectorIn.Close()
						_ = inspectorIn.Close()
					}()
					w := io.MultiWriter(dissectorIn, inspectorIn)
					for {
						_, msg, err := client.ReadMessage()
						if err != nil {
							return
						}
						if _, err := w.Write(msg); err != nil {
							return
						}
					}
				}()

				packet := fmt.Sprintf(`lo 127\.0\.0\.1 %d %x\n`, wantedPort, marker)
				for range 3 {
					Eventually(packets).Within(10 * time.Second).Should(gbytes.Say(packet))
				}

				Expect(client.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"))).To(Succeed())
				Eventually(pumped).Within(5 * time.Second).Should(BeClosed())
				Expect(dissector.Wait()).To(Succeed())
				Expect(inspector.Wait()).To(Succeed())
				Eventually(done).Within(5 * time.Second).Should(BeClosed())

				Expect(string(packets.Contents())).To(MatchRegexp(`\A(`+packet+`)+\z`),
					"only packets matching the capture filter")
				// capinfos shows the SHB comment with its line breaks turned into spaces.
				Expect(string(infos.Contents())).To(MatchRegexp(`Capture comment:\s+` +
					`--- # capture target information ` +
					`container-name: chatty container-type: docker node-name: "" ` +
					regexp.QuoteMeta("capture-filter: "+filter) + ` no-promiscuous-mode: true`))
			})

			DescribeTable("reporting capture program failures",
				func(params string, reason string) {
					wsurl, done := serveCapture(NewCaptureHandler(""))
					_, err := readUntilClosed(dial(wsurl + query + params))
					Expect(err).To(MatchError(&websocket.CloseError{
						Code: websocket.CloseInternalServerErr,
						Text: reason,
					}))
					Eventually(done).Within(5 * time.Second).Should(BeClosed())
				},
				Entry("invalid capture filter", "&nif=lo&filter="+url.QueryEscape("not(valid"),
					`Invalid capture filter "not(valid" for interface 'lo'.`),
				Entry("unknown network interface", "&nif=nosuchnif",
					`There is no device named "nosuchnif".`),
			)

			It("reports failing to start the capture program", func() {
				wsurl, done := serveCapture(&captureHandler{program: "/nonexistent"})
				_, err := readUntilClosed(dial(wsurl + query + "&nif=lo"))
				Expect(err).To(MatchError(&websocket.CloseError{
					Code: websocket.CloseInternalServerErr,
					Text: "cannot start capture process",
				}))
				Eventually(done).Should(BeClosed())
			})

		})

	})

})

// serveCapture serves a single request using the passed capture handler until
// the end of the current spec, returning the websocket base URL as well as a
// channel that gets closed after the handler has returned.
func serveCapture(h http.Handler) (string, <-chan struct{}) {
	GinkgoHelper()
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		defer close(done)
		h.ServeHTTP(w, req)
	}))
	DeferCleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http"), done
}

// dial connects to the specified websocket URL until the end of the current
// spec.
func dial(wsurl string) *websocket.Conn {
	GinkgoHelper()
	client, _ := Successful2R(websocket.DefaultDialer.Dial(wsurl, nil))
	DeferCleanup(func() { _ = client.Close() })
	Expect(client.SetReadDeadline(time.Now().Add(10 * time.Second))).To(Succeed())
	return client
}

// readUntilClosed returns all data messages received until the websocket
// connection fails or gets closed, as well as the final error.
func readUntilClosed(client *websocket.Conn) ([]byte, error) {
	var stream []byte
	for {
		_, msg, err := client.ReadMessage()
		if err != nil {
			return stream, err
		}
		stream = append(stream, msg...)
	}
}

// needsPath returns the path of the specified program, skipping the current
// spec if it is unavailable.
func needsPath(program string) string {
	GinkgoHelper()
	path, err := exec.LookPath(program)
	if err != nil {
		Skip("needs " + program)
	}
	return path
}

// otherInterface returns the name of a network interface that is up and not
// the loopback interface, skipping the current spec if there is none.
func otherInterface() string {
	GinkgoHelper()
	for _, nif := range Successful(net.Interfaces()) {
		if nif.Flags&net.FlagUp != 0 && nif.Flags&net.FlagLoopback == 0 {
			return nif.Name
		}
	}
	Skip("needs a non-loopback network interface")
	return ""
}

// udpSink returns the address of a loopback UDP socket that silently swallows
// everything sent to it until the end of the current spec.
func udpSink() net.Addr {
	GinkgoHelper()
	sink := Successful(net.ListenPacket("udp", "127.0.0.1:0"))
	DeferCleanup(sink.Close)
	return sink.LocalAddr()
}

// chatter periodically sends the payload to the specified UDP addresses until
// the end of the current spec.
func chatter(payload []byte, addrs ...net.Addr) {
	GinkgoHelper()
	sender := Successful(net.ListenPacket("udp", "127.0.0.1:0"))
	DeferCleanup(sender.Close)
	stop := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				for _, addr := range addrs {
					_, _ = sender.WriteTo(payload, addr)
				}
			}
		}
	}()
	DeferCleanup(func() {
		close(stop)
		<-stopped
	})
}
