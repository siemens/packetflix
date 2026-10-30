// (c) Siemens AG 2026
//
// SPDX-License-Identifier: MIT

package service

import (
	"github.com/spf13/cobra"
	"github.com/thediveo/clippy/cliplugin"
	"github.com/thediveo/go-plugger/v3"

	"github.com/siemens/packetflix"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("service port", func() {

	It("registers as a CLI setup plugin", func() {
		Expect(plugger.Group[cliplugin.SetupCLI]().Plugins()).To(ContainElement("service"))
	})

	It("returns zero without the flag", func() {
		Expect(GetPort(&cobra.Command{})).To(BeZero())
	})

	DescribeTable("getting the port",
		func(args []string, expected uint16) {
			var port uint16
			root := &cobra.Command{
				Use: "root",
				Run: func(cmd *cobra.Command, _ []string) { port = GetPort(cmd) },
			}
			root.SetOut(GinkgoWriter)
			root.SetErr(GinkgoWriter)
			setupCLI(root)
			root.SetArgs(args)
			Expect(root.Execute()).To(Succeed())
			Expect(port).To(Equal(expected))
		},
		Entry("default", nil, uint16(packetflix.DefaultPacketFlixServicePort)),
		Entry("explicit", []string{"--" + PortFlag, "6666"}, uint16(6666)),
		Entry("shorthand", []string{"-" + PortShorthandFlag, "42"}, uint16(42)),
	)

	It("rejects an invalid port", func() {
		root := &cobra.Command{Use: "root", Run: func(*cobra.Command, []string) {}}
		root.SetOut(GinkgoWriter)
		root.SetErr(GinkgoWriter)
		setupCLI(root)
		root.SetArgs([]string{"--" + PortFlag, "65536"})
		Expect(root.Execute()).To(MatchError(ContainSubstring("invalid argument")))
	})

})
