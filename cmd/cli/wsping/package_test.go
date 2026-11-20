// (c) Siemens AG 2026
//
// SPDX-License-Identifier: MIT

package wsping

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestCmdCliWsping(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "packetflix cmd/cli/wsping package")
}
