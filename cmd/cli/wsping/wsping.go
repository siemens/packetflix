// (c) Siemens AG 2026
//
// SPDX-License-Identifier: MIT

package wsping

import (
	"time"

	"github.com/spf13/cobra"
	"github.com/thediveo/clippy/cliplugin"
	"github.com/thediveo/go-plugger/v3"

	"github.com/siemens/packetflix"
)

// IntervalFlag is the name of the CLI flag provided in this package.
const IntervalFlag = "wsping-interval"

func init() {
	plugger.Group[cliplugin.SetupCLI]().Register(
		setupCLI, plugger.WithPlugin("wsping"))
}

// setupCLI adds the ping interval flag.
func setupCLI(cmd *cobra.Command) {
	cmd.PersistentFlags().Duration(IntervalFlag, packetflix.DefaultPingInterval,
		"interval of websocket pings on capture connections; 0 disables pings")
}

// Interval returns the websocket ping interval specified for the passed
// command. It returns zero, thus no pings, if the flag isn't present.
func Interval(cmd *cobra.Command) time.Duration {
	interval, _ := cmd.Flags().GetDuration(IntervalFlag)
	return interval
}
