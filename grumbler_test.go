// (c) Siemens AG 2023
//
// SPDX-License-Identifier: MIT

package packetflix

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("grumbler", func() {

	It("ignores mumbling and keeps the first error line as reason", func() {
		g := NewGrumbler(NewWSConn())
		grumble := []byte("Capturing on 'eth0'\ndumpcap: The capture session could not be initiated\nmore details\n")
		Expect(g.Write(grumble)).To(Equal(len(grumble)))
		Expect(g.Reason()).To(Equal("The capture session could not be initiated"))
	})

	It("assembles a reason split across multiple writes", func() {
		g := NewGrumbler(NewWSConn())
		for _, grumble := range []string{"dump", "cap: oo", "ps\n"} {
			Expect(g.Write([]byte(grumble))).To(Equal(len(grumble)))
		}
		Expect(g.Reason()).To(Equal("oops"))
	})

	It("strips carriage returns", func() {
		g := NewGrumbler(NewWSConn())
		Expect(g.Write([]byte("dumpcap: oops\r\n"))).Error().NotTo(HaveOccurred())
		Expect(g.Reason()).To(Equal("oops"))
	})

	It("has no reason when there is only mumbling", func() {
		g := NewGrumbler(NewWSConn())
		Expect(g.Write([]byte("Capturing on 'eth0'\nFile: -\n"))).Error().NotTo(HaveOccurred())
		Expect(g.Reason()).To(BeEmpty())
	})

	It("keeps only the first error line, even across later writes", func() {
		g := NewGrumbler(NewWSConn())
		Expect(g.Write([]byte("dumpcap: oops\n"))).Error().NotTo(HaveOccurred())
		Expect(g.Write([]byte("more details\n"))).Error().NotTo(HaveOccurred())
		Expect(g.Reason()).To(Equal("oops"))
	})

})
