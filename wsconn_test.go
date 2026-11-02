// (c) Siemens AG 2023
//
// SPDX-License-Identifier: MIT

package packetflix

import (
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/onsi/gomega/gbytes"

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

})
