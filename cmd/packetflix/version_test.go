// (c) Siemens AG 2026
//
// SPDX-License-Identifier: MIT

package main

import (
	"runtime/debug"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("module version", func() {

	It("has a version", func() {
		Expect(buildVersion()).NotTo(BeEmpty())
	})

	It("prefers a version set at link time", func() {
		DeferCleanup(func(old string) { version = old }, version)
		version = "v1.2.3-4-gabcdef0"
		Expect(buildVersion()).To(Equal("1.2.3-4-gabcdef0"))
	})

	DescribeTable("from build information",
		func(bi *debug.BuildInfo, expected string) {
			Expect(moduleVersion(bi)).To(Equal(expected))
		},
		Entry("without build information", nil, "(devel)"),
		Entry("as main module", &debug.BuildInfo{
			Main: debug.Module{Path: modulePath, Version: "v0.10.0"},
		}, "0.10.0"),
		Entry("as main module with VCS pseudo-version", &debug.BuildInfo{
			Main: debug.Module{Path: modulePath, Version: "v0.10.1-0.20261030120000-abcdef123456+dirty"},
		}, "0.10.1-0.20261030120000-abcdef123456+dirty"),
		Entry("as development main module", &debug.BuildInfo{
			Main: debug.Module{Path: modulePath, Version: "(devel)"},
		}, "(devel)"),
		Entry("as dependency", &debug.BuildInfo{
			Main: debug.Module{Path: "example.org/foo", Version: "v1.0.0"},
			Deps: []*debug.Module{
				{Path: "example.org/bar", Version: "v0.1.0"},
				{Path: modulePath, Version: "v0.9.7"},
			},
		}, "0.9.7"),
		Entry("as replaced dependency", &debug.BuildInfo{
			Main: debug.Module{Path: "example.org/foo"},
			Deps: []*debug.Module{
				{Path: modulePath, Version: "v0.9.7", Replace: &debug.Module{Path: "example.org/fork", Version: "v0.9.8"}},
			},
		}, "0.9.8"),
		Entry("as dependency replaced by local directory", &debug.BuildInfo{
			Main: debug.Module{Path: "example.org/foo"},
			Deps: []*debug.Module{
				{Path: modulePath, Version: "v0.9.7", Replace: &debug.Module{Path: "../packetflix"}},
			},
		}, "(devel)"),
		Entry("when missing", &debug.BuildInfo{
			Main: debug.Module{Path: "example.org/foo", Version: "v1.0.0"},
		}, "(devel)"),
	)

})
