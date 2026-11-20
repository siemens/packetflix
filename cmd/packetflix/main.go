// (c) Siemens AG 2023
//
// SPDX-License-Identifier: MIT

package main

import (
	"os"

	"github.com/siemens/packetflix/cmd/packetflix/rootcmd"
)

// version is only ever set at link time for builds lacking VCS information,
// such as Docker builds: -ldflags "-X main.version=…".
var version string

func main() {
	if err := rootcmd.Execute(rootcmd.New(rootcmd.Variant{Version: version})); err != nil {
		os.Exit(1)
	}
}
