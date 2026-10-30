// (c) Siemens AG 2023
//
// SPDX-License-Identifier: MIT

package packetflix

import (
	"github.com/gorilla/websocket"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/thediveo/success"
)

var _ = Describe("piper", func() {

	It("pipes data as binary websocket messages", func() {
		data := []byte("packet")
		client, done := newWSPair(func(conn *WSConn) {
			piper := NewPiper(conn)
			Expect(piper.Write(data)).To(Equal(len(data)))
			Expect(piper.Failed).To(BeFalse())
		})
		msgtype, msg := Successful2R(client.ReadMessage())
		Expect(msgtype).To(Equal(websocket.BinaryMessage))
		Expect(msg).To(Equal(data))
		Eventually(done).Should(BeClosed())
	})

	It("drops data on a broken websocket, marking it as failed", func() {
		_, done := newWSPair(func(conn *WSConn) {
			Expect(conn.UnderlyingConn().Close()).To(Succeed())
			piper := NewPiper(conn)
			Expect(piper.Write([]byte("lost"))).To(Equal(4))
			Expect(piper.Failed).To(BeTrue())
		})
		Eventually(done).Should(BeClosed())
	})

	It("drops data once the websocket is closing", func() {
		_, done := newWSPair(func(conn *WSConn) {
			Expect(conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))).To(Succeed())
			piper := NewPiper(conn)
			Expect(piper.Write([]byte("lost"))).To(Equal(4))
			Expect(piper.Write([]byte("lost, too"))).To(Equal(9))
			Expect(piper.Failed).To(BeTrue())
		})
		Eventually(done).Should(BeClosed())
	})

})
