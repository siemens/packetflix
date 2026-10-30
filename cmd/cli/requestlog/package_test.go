// (c) Siemens AG 2026
//
// SPDX-License-Identifier: MIT

package requestlog

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestCmdCliRequestlog(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "packetflix cmd/cli/requestlog package")
}
