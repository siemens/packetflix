// (c) Siemens AG 2023
//
// SPDX-License-Identifier: MIT

package packetflix

import (
	"io"
	"net"
	"os/exec"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/onsi/gomega/gbytes"
	"github.com/thediveo/nonstd/xslices"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("websocket connection", func() {

	It("has a human-friendly ID", func() {
		conn := NewWSConn()
		Expect(conn.ID).To(MatchRegexp(`^[a-z]+-[a-z]+$`))
		Expect(NewWSConn().ID).NotTo(Equal(conn.ID))
	})

	When("terminating its capture process", func() {

		It("does nothing without a process", func() {
			Expect(NewWSConn().Terminate).NotTo(Panic())
		})

		It("signals the process only once", func() {
			stdout := gbytes.NewBuffer()
			cmd := exec.Command("/bin/sh", "-c", `trap "echo TERM" TERM; echo ready; while :; do sleep 0.1; done`)
			cmd.Stdout = stdout
			Expect(cmd.Start()).To(Succeed())
			DeferCleanup(func() {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			})
			Eventually(stdout).Should(gbytes.Say("ready"))

			conn := NewWSConn()
			conn.Process = cmd.Process
			conn.Terminate()
			conn.Terminate()
			Eventually(stdout).Should(gbytes.Say("TERM"))
			Consistently(stdout).WithTimeout(500 * time.Millisecond).ShouldNot(gbytes.Say("TERM"))
		})

		It("tolerates an already finished process", func() {
			cmd := exec.Command("/bin/true")
			Expect(cmd.Run()).To(Succeed())
			conn := NewWSConn()
			conn.Process = cmd.Process
			Expect(conn.Terminate).NotTo(Panic())
		})

	})

	It("completes a graceful close initiated by the server", func() {
		client, done := newWSPair(func(conn *WSConn) {
			conn.GracefullyClose(websocket.CloseNormalClosure, "bye")
			Expect(conn.state).To(Equal(WSConnClosed))
		})
		_, _, err := client.ReadMessage()
		Expect(err).To(MatchError(
			&websocket.CloseError{Code: websocket.CloseNormalClosure, Text: "bye"}))
		Eventually(done).Should(BeClosed())
	})

	It("acknowledges a close by the client and terminates the capture process", func() {
		cmd := exec.Command("/bin/sleep", "60")
		Expect(cmd.Start()).To(Succeed())
		DeferCleanup(func() {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		})
		client, done := newWSPair(func(conn *WSConn) {
			conn.Process = cmd.Process
			conn.Watch()
			Expect(conn.state).To(Equal(WSConnClosed))
		})
		Expect(client.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"))).To(Succeed())
		_, _, err := client.ReadMessage()
		// gorilla's default close handler already acks before Watch sees the close.
		Expect(err).To(SatisfyAll(
			BeAssignableToTypeOf(&websocket.CloseError{}),
			HaveField("Code", websocket.CloseNormalClosure)))
		Eventually(done).Should(BeClosed())
		Expect(cmd.Wait()).To(MatchError(ContainSubstring("signal: terminated")))
	})

	It("terminates the capture process when the client drops the connection", func() {
		cmd := exec.Command("/bin/sleep", "60")
		Expect(cmd.Start()).To(Succeed())
		DeferCleanup(func() {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		})
		client, done := newWSPair(func(conn *WSConn) {
			conn.Process = cmd.Process
			conn.Watch()
		})
		Expect(client.UnderlyingConn().Close()).To(Succeed())
		Eventually(done).Should(BeClosed())
		Expect(cmd.Wait()).To(MatchError(ContainSubstring("signal: terminated")))
	})

	It("terminates the capture process when it cannot initiate a graceful close", func() {
		cmd := exec.Command("/bin/sleep", "60")
		Expect(cmd.Start()).To(Succeed())
		DeferCleanup(func() {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		})
		_, done := newWSPair(func(conn *WSConn) {
			conn.Process = cmd.Process
			Expect(conn.UnderlyingConn().Close()).To(Succeed())
			conn.InitiateGracefulClose(websocket.CloseNormalClosure, "bye")
			Expect(conn.state).To(Equal(WSConnClosed))
		})
		Eventually(done).Should(BeClosed())
		Expect(cmd.Wait()).To(MatchError(ContainSubstring("signal: terminated")))
	})

	It("does not initiate a graceful close when already closing", func() {
		client, done := newWSPair(func(conn *WSConn) {
			conn.state = WSConnClosing
			conn.InitiateGracefulClose(websocket.CloseNormalClosure, "bye")
		})
		_, _, err := client.ReadMessage()
		Expect(err).To(MatchError(
			&websocket.CloseError{Code: websocket.CloseAbnormalClosure, Text: io.ErrUnexpectedEOF.Error()}))
		Eventually(done).Should(BeClosed())
	})

	It("does not run a graceful close when already closing", func() {
		client, done := newWSPair(func(conn *WSConn) {
			conn.state = WSConnClosing
			conn.GracefullyClose(websocket.CloseNormalClosure, "bye")
		})
		_, _, err := client.ReadMessage()
		Expect(err).To(MatchError(
			&websocket.CloseError{Code: websocket.CloseAbnormalClosure, Text: io.ErrUnexpectedEOF.Error()}))
		Eventually(done).Should(BeClosed())
	})

	It("passes overly long close reasons truncated to the client", func() {
		client, done := newWSPair(func(conn *WSConn) {
			conn.GracefullyClose(websocket.CloseNormalClosure, strings.Repeat("x", 200))
		})
		_, _, err := client.ReadMessage()
		Expect(err).To(MatchError(
			&websocket.CloseError{Code: websocket.CloseNormalClosure, Text: strings.Repeat("x", 123)}))
		Eventually(done).Should(BeClosed())
	})

	It("doesn't truncate close reasons in the middle of a character", func() {
		client, done := newWSPair(func(conn *WSConn) {
			conn.GracefullyClose(websocket.CloseNormalClosure, strings.Repeat("x", 122)+"ää")
		})
		_, _, err := client.ReadMessage()
		Expect(err).To(MatchError(
			&websocket.CloseError{Code: websocket.CloseNormalClosure, Text: strings.Repeat("x", 122)}))
		Eventually(done).Should(BeClosed())
	})

	When("pinging", func() {

		It("pings at the interval until the client closes, and not afterwards", func() {
			client, done := newWSPair(func(conn *WSConn) {
				defer conn.startPinging(50 * time.Millisecond)()
				conn.Watch()
			})
			frames := readFrames(client.UnderlyingConn(), time.Now().Add(400*time.Millisecond))
			Expect(len(frames)).To(BeNumerically(">=", 4))
			Expect(opcodes(frames)).To(HaveEach(websocket.PingMessage))
			Expect(frames[len(frames)-1].at.Sub(frames[0].at) / time.Duration(len(frames)-1)).
				To(BeNumerically("~", 50*time.Millisecond, 25*time.Millisecond))

			Expect(client.WriteControl(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"),
				time.Now().Add(time.Second))).To(Succeed())
			Expect(opcodes(readFrames(client.UnderlyingConn(), time.Now().Add(time.Second)))).
				To(SatisfyAny(BeEmpty(), Equal([]int{websocket.PingMessage, websocket.CloseMessage}),
					Equal([]int{websocket.CloseMessage})))
			Eventually(done).Should(BeClosed())
		})

		It("stops pinging when the server begins closing", func() {
			client, done := newWSPair(func(conn *WSConn) {
				defer conn.startPinging(50 * time.Millisecond)()
				time.Sleep(300 * time.Millisecond)
				conn.InitiateGracefulClose(websocket.CloseNormalClosure, "bye")
				conn.Watch()
			})
			// Never acknowledge the close, so the server keeps waiting.
			codes := opcodes(readFrames(client.UnderlyingConn(), time.Now().Add(time.Second)))
			Expect(codes).To(ContainElement(websocket.PingMessage))
			Expect(codes[len(codes)-1]).To(Equal(websocket.CloseMessage))
			Expect(codes[:len(codes)-1]).To(HaveEach(websocket.PingMessage))
			Expect(client.UnderlyingConn().Close()).To(Succeed())
			Eventually(done).Should(BeClosed())
		})

		It("stops pinging when asked to, also repeatedly", func() {
			stopped := make(chan time.Time, 1)
			client, done := newWSPair(func(conn *WSConn) {
				stop := conn.startPinging(50 * time.Millisecond)
				time.Sleep(200 * time.Millisecond)
				stop()
				stopped <- time.Now()
				Expect(stop).NotTo(Panic())
				conn.Watch()
			})
			frames := readFrames(client.UnderlyingConn(), time.Now().Add(600*time.Millisecond))
			var stoppedAt time.Time
			Expect(stopped).To(Receive(&stoppedAt))
			Expect(frames).NotTo(BeEmpty())
			Expect(xslices.Map(frames, func(f frame) time.Time { return f.at })).
				To(HaveEach(BeTemporally("<", stoppedAt.Add(10*time.Millisecond))))
			Expect(client.UnderlyingConn().Close()).To(Succeed())
			Eventually(done).Should(BeClosed())
		})

		It("terminates the capture process when a ping cannot be sent", func() {
			cmd := exec.Command("/bin/sleep", "60")
			Expect(cmd.Start()).To(Succeed())
			DeferCleanup(func() {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			})
			client, done := newWSPair(func(conn *WSConn) {
				conn.Process = cmd.Process
				defer conn.startPinging(50 * time.Millisecond)()
				Eventually(func() int {
					code, _ := conn.closeCause()
					return code
				}).Should(Equal(websocket.CloseAbnormalClosure))
			})
			// Resetting the connection makes the next ping fail.
			tcpconn := client.UnderlyingConn().(*net.TCPConn)
			Expect(tcpconn.SetLinger(0)).To(Succeed())
			Expect(tcpconn.Close()).To(Succeed())
			Eventually(done).Should(BeClosed())
			Expect(cmd.Wait()).To(MatchError(ContainSubstring("signal: terminated")))
		})

	})

})
