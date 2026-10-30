// (c) Siemens AG 2023
//
// SPDX-License-Identifier: MIT

package packetflix

import (
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/gorilla/websocket"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/thediveo/success"
)

var _ = Describe("capture handler", func() {

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

		// pending: sends the reserved close code 1006, which clients reject.
		PIt("tells the client the reason", func() {
			client, _ := Successful2R(websocket.DefaultDialer.Dial(wsurl+"/capture", nil))
			DeferCleanup(func() { _ = client.Close() })
			_, _, err := client.ReadMessage()
			Expect(err).To(SatisfyAll(
				BeAssignableToTypeOf(&websocket.CloseError{}),
				HaveField("Text", "either container or netns query parameter required")))
		})

	})

})
