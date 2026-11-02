// (c) Siemens AG 2023
//
// SPDX-License-Identifier: MIT

package packetflix

import (
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/siemens/csharg/v2/api"

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
			[]string{"-p", "-i", "eth0", "-i", "lo", "-f", "tcp"}),
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

})
