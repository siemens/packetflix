// (c) Siemens AG 2026
//
// SPDX-License-Identifier: MIT

package discovery

import (
	"github.com/spf13/cobra"
	"github.com/thediveo/clippy/cliplugin"
	"github.com/thediveo/go-plugger/v3"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("discovery service", func() {

	It("registers as a CLI setup plugin", func() {
		Expect(plugger.Group[cliplugin.SetupCLI]().Plugins()).To(ContainElement("discovery"))
	})

	It("doesn't proxy without the flag", func() {
		Expect(IsProxying(&cobra.Command{})).To(BeFalse())
	})

	DescribeTable("getting the discovery settings",
		func(args []string, addr string, proxy bool) {
			var address string
			var proxying bool
			root := &cobra.Command{
				Use: "root",
				Run: func(cmd *cobra.Command, _ []string) {
					address = Address(cmd)
					proxying = IsProxying(cmd)
				},
			}
			root.SetOut(GinkgoWriter)
			root.SetErr(GinkgoWriter)
			setupCLI(root)
			root.SetArgs(args)
			Expect(root.Execute()).To(Succeed())
			Expect(address).To(Equal(addr))
			Expect(proxying).To(Equal(proxy))
		},
		Entry("defaults", nil, "127.0.0.1:5000", false),
		Entry("explicit",
			[]string{"--" + ServiceFlag, "gostwire.ghost-in-da-box", "--" + PortFlag, "5005", "--" + ProxyFlag},
			"gostwire.ghost-in-da-box:5005", true),
		Entry("IPv6", []string{"--" + ServiceFlag, "::1"}, "[::1]:5000", false),
	)

	It("rejects an invalid port", func() {
		root := &cobra.Command{Use: "root", Run: func(*cobra.Command, []string) {}}
		root.SetOut(GinkgoWriter)
		root.SetErr(GinkgoWriter)
		setupCLI(root)
		root.SetArgs([]string{"--" + PortFlag, "foo"})
		Expect(root.Execute()).To(MatchError(ContainSubstring("invalid argument")))
	})

})
