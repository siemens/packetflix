// (c) Siemens AG 2026
//
// SPDX-License-Identifier: MIT

package requestlog

import (
	"github.com/spf13/cobra"
	"github.com/thediveo/clippy/cliplugin"
	"github.com/thediveo/go-plugger/v3"
)

// Names of the CLI flags provided in this package.
const (
	LogRequestsFlag = "log-requests"
	LogHeadersFlag  = "log-headers"
)

func init() {
	plugger.Group[cliplugin.SetupCLI]().Register(
		setupCLI, plugger.WithPlugin("requestlog"))
}

// setupCLI adds the request logging flags.
func setupCLI(cmd *cobra.Command) {
	pf := cmd.PersistentFlags()
	pf.Bool(LogRequestsFlag, false, "log frontend HTTP/WS requests")
	pf.Bool(LogHeadersFlag, false, "log frontend HTTP/WS request headers")
}

// IsLogging returns true if requests should be logged, where logging request
// headers implies logging requests.
func IsLogging(cmd *cobra.Command) bool {
	logreqs, _ := cmd.Flags().GetBool(LogRequestsFlag)
	loghdrs, _ := cmd.Flags().GetBool(LogHeadersFlag)
	return logreqs || loghdrs
}
