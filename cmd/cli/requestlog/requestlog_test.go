// (c) Siemens AG 2026
//
// SPDX-License-Identifier: MIT

package requestlog

import (
	"github.com/spf13/cobra"
	"github.com/thediveo/clippy/cliplugin"
	"github.com/thediveo/go-plugger/v3"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("request logging", func() {

	It("registers as a CLI setup plugin", func() {
		Expect(plugger.Group[cliplugin.SetupCLI]().Plugins()).To(ContainElement("requestlog"))
	})

	It("doesn't log without the flags", func() {
		Expect(IsLogging(&cobra.Command{})).To(BeFalse())
	})

	DescribeTable("enabling request logging",
		func(args []string, expected bool) {
			var logging bool
			root := &cobra.Command{
				Use: "root",
				Run: func(cmd *cobra.Command, _ []string) { logging = IsLogging(cmd) },
			}
			root.SetOut(GinkgoWriter)
			root.SetErr(GinkgoWriter)
			setupCLI(root)
			root.SetArgs(args)
			Expect(root.Execute()).To(Succeed())
			Expect(logging).To(Equal(expected))
		},
		Entry("default", nil, false),
		Entry("requests", []string{"--" + LogRequestsFlag}, true),
		Entry("headers imply requests", []string{"--" + LogHeadersFlag}, true),
	)

})
