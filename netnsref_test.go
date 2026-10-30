// (c) Siemens AG 2023
//
// SPDX-License-Identifier: MIT

package main

import (
	"os"

	"github.com/thediveo/spacetest/bindmount"
	"github.com/thediveo/spacetest/netns"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("network namespace paths", func() {

	It("finds a process referencing the network namespace", func() {
		netnsino := netns.CurrentIno()
		ref := netnsPath(netnsino)
		Expect(ref).To(MatchRegexp(`^/proc/\d+/ns/net$`))
		Expect(netns.Ino(ref)).To(Equal(netnsino))
	})

	It("returns nothing for an unknown network namespace", func() {
		Expect(netnsPath(1)).To(BeEmpty())
	})

	It("finds bind-mounted network namespaces without any processes", func() {
		if os.Geteuid() != 0 {
			Skip("needs root")
		}
		ref := bindmount.NewTransient(netns.NewTransient())
		Expect(netnsPath(netns.Ino(ref))).To(Equal(ref))
	})

})
