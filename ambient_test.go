// (c) Siemens AG 2023
//
// SPDX-License-Identifier: MIT

package main

import (
	"os/exec"
	"runtime"
	"strings"

	"github.com/thediveo/caps/v2"
	"github.com/thediveo/nonstd/xslices"
	"github.com/thediveo/testily/concur"
	"golang.org/x/sys/unix"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/thediveo/success"
)

var _ = Describe("ambient capabilities", func() {

	It("does nothing when given no capabilities", func() {
		concur.GoSync(func() {
			defer GinkgoRecover()
			// never unlocked, so the runtime discards the thread with its caps.
			runtime.LockOSThread()

			taskcaps := Successful(caps.OfCurrentTask())
			Expect(SetAmbient()).To(Succeed())
			Expect(caps.OfCurrentTask()).To(Equal(taskcaps))
			Expect(unix.PrctlRetInt(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_IS_SET, caps.CAP_NET_ADMIN, 0, 0)).To(BeZero())
			Expect(unix.PrctlRetInt(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_IS_SET, caps.CAP_NET_RAW, 0, 0)).To(BeZero())
		})
	})

	It("rejects capabilities that are not permitted", func() {
		concur.GoSync(func() {
			defer GinkgoRecover()
			// never unlocked, so the runtime discards the thread with its caps.
			runtime.LockOSThread()

			taskcaps := Successful(caps.OfCurrentTask())
			if taskcaps.Permitted().Has(caps.CAP_NET_RAW) {
				Expect(taskcaps.Permitted().Drop(caps.CAP_NET_RAW).
					Effective().Drop(caps.CAP_NET_RAW).
					Inheritable().Drop(caps.CAP_NET_RAW).
					ApplyToCurrentTask()).Error().NotTo(HaveOccurred())
			}
			Expect(SetAmbient(caps.CAP_NET_RAW)).To(
				MatchError(ContainSubstring("CAP_NET_RAW not permitted")))
			Expect(unix.PrctlRetInt(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_IS_SET, caps.CAP_NET_RAW, 0, 0)).To(BeZero())
		})
	})

	It("raises permitted capabilities as ambient and passes them on to child processes", func() {
		concur.GoSync(func() {
			defer GinkgoRecover()
			// never unlocked, so the runtime discards the thread with its caps.
			runtime.LockOSThread()

			permitted := Successful(caps.OfCurrentTask()).Permitted()
			if !permitted.Has(caps.CAP_NET_ADMIN) || !permitted.Has(caps.CAP_NET_RAW) {
				Skip("needs CAP_NET_ADMIN and CAP_NET_RAW permitted, run as root")
			}

			// SetAmbient must raise the inheritable caps itself, so start without any.
			Expect(Successful(caps.OfCurrentTask()).Inheritable().Clear().ApplyToCurrentTask()).
				Error().NotTo(HaveOccurred())
			Expect(Successful(caps.OfCurrentTask()).Inheritable().IsEmpty()).To(BeTrue())

			Expect(SetAmbient(caps.CAP_NET_ADMIN, caps.CAP_NET_RAW)).To(Succeed())
			inheritable := Successful(caps.OfCurrentTask()).Inheritable()
			Expect(inheritable.Has(caps.CAP_NET_ADMIN)).To(BeTrue())
			Expect(inheritable.Has(caps.CAP_NET_RAW)).To(BeTrue())
			Expect(unix.PrctlRetInt(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_IS_SET, caps.CAP_NET_ADMIN, 0, 0)).To(Equal(1))
			Expect(unix.PrctlRetInt(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_IS_SET, caps.CAP_NET_RAW, 0, 0)).To(Equal(1))

			status := string(Successful(exec.Command("/bin/cat", "/proc/self/status").Output()))
			capamb := Allright(xslices.FindFuncOk(strings.Split(status, "\n"), func(l string) bool {
				return strings.HasPrefix(l, "CapAmb:")
			}))
			childAmbient := Successful(caps.CapabilitiesFromHex(strings.TrimSpace(strings.TrimPrefix(capamb, "CapAmb:"))))
			Expect(childAmbient.Has(caps.CAP_NET_ADMIN)).To(BeTrue())
			Expect(childAmbient.Has(caps.CAP_NET_RAW)).To(BeTrue())
		})
	})

	It("reports when the kernel refuses to raise ambient capabilities", func() {
		concur.GoSync(func() {
			defer GinkgoRecover()
			// never unlocked, so the runtime discards the thread with its caps.
			runtime.LockOSThread()

			taskcaps := Successful(caps.OfCurrentTask())
			if !taskcaps.Effective().Has(caps.CAP_SETPCAP) || !taskcaps.Permitted().Has(caps.CAP_NET_RAW) {
				Skip("needs CAP_SETPCAP effective and CAP_NET_RAW permitted, run as root")
			}

			securebits := Successful(unix.PrctlRetInt(unix.PR_GET_SECUREBITS, 0, 0, 0, 0))
			const secbitNoCapAmbientRaise = 1 << 6 // from linux/securebits.h, missing in x/sys/unix
			Expect(unix.Prctl(unix.PR_SET_SECUREBITS,
				uintptr(securebits|secbitNoCapAmbientRaise), 0, 0, 0)).To(Succeed())

			Expect(SetAmbient(caps.CAP_NET_RAW)).To(SatisfyAll(
				MatchError(unix.EPERM),
				MatchError(ContainSubstring("cannot raise ambient capability CAP_NET_RAW"))))
			Expect(unix.PrctlRetInt(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_IS_SET, caps.CAP_NET_RAW, 0, 0)).To(BeZero())
		})
	})

})
