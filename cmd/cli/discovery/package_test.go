// (c) Siemens AG 2026
//
// SPDX-License-Identifier: MIT

package discovery

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestCmdCliDiscovery(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "packetflix cmd/cli/discovery package")
}
