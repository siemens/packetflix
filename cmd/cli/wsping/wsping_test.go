// (c) Siemens AG 2026
//
// SPDX-License-Identifier: MIT

package wsping

import (
	"time"

	"github.com/spf13/cobra"
	"github.com/thediveo/clippy/cliplugin"
	"github.com/thediveo/go-plugger/v3"

	"github.com/siemens/packetflix"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ping interval", func() {

	It("registers as a CLI setup plugin", func() {
		Expect(plugger.Group[cliplugin.SetupCLI]().Plugins()).To(ContainElement("wsping"))
	})

	It("doesn't ping without the flag", func() {
		Expect(Interval(&cobra.Command{})).To(BeZero())
	})

	DescribeTable("getting the interval",
		func(args []string, expected time.Duration) {
			var interval time.Duration
			root := &cobra.Command{
				Use: "root",
				Run: func(cmd *cobra.Command, _ []string) { interval = Interval(cmd) },
			}
			root.SetOut(GinkgoWriter)
			root.SetErr(GinkgoWriter)
			setupCLI(root)
			root.SetArgs(args)
			Expect(root.Execute()).To(Succeed())
			Expect(interval).To(Equal(expected))
		},
		Entry("default", []string{}, packetflix.DefaultPingInterval),
		Entry("explicit", []string{"--" + IntervalFlag, "30s"}, 30*time.Second),
		Entry("disabled", []string{"--" + IntervalFlag, "0"}, time.Duration(0)),
	)

	It("rejects an invalid interval", func() {
		root := &cobra.Command{Use: "root", Run: func(*cobra.Command, []string) {}}
		root.SetOut(GinkgoWriter)
		root.SetErr(GinkgoWriter)
		setupCLI(root)
		root.SetArgs([]string{"--" + IntervalFlag, "foo"})
		Expect(root.Execute()).To(MatchError(ContainSubstring("invalid argument")))
	})

})
