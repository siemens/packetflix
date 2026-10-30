// (c) Siemens AG 2023
//
// SPDX-License-Identifier: MIT

package main

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestPacketflix(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "packetflix package")
}

// secbitNoCapAmbientRaise is SECBIT_NO_CAP_AMBIENT_RAISE from
// linux/securebits.h, which golang.org/x/sys/unix lacks.
const secbitNoCapAmbientRaise = 1 << 6
