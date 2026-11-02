// (c) Siemens AG 2026
//
// SPDX-License-Identifier: MIT

package service

import (
	"github.com/spf13/cobra"
	"github.com/thediveo/clippy/cliplugin"
	"github.com/thediveo/go-plugger/v3"

	"github.com/siemens/packetflix"
)

// Names of the CLI flags provided in this package.
const (
	PortFlag          = "port"
	PortShorthandFlag = "p"
)

func init() {
	plugger.Group[cliplugin.SetupCLI]().Register(
		setupCLI, plugger.WithPlugin("service"))
}

// setupCLI adds the service port flag.
func setupCLI(cmd *cobra.Command) {
	cmd.PersistentFlags().Uint16P(PortFlag, PortShorthandFlag, packetflix.DefaultPacketFlixServicePort,
		"port to expose capture service on")
}

// GetPort returns the service port specified for the passed command. It returns
// zero if the flag isn't present.
func GetPort(cmd *cobra.Command) uint16 {
	port, _ := cmd.Flags().GetUint16(PortFlag)
	return port
}
