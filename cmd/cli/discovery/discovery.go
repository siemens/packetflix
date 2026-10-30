// (c) Siemens AG 2026
//
// SPDX-License-Identifier: MIT

package discovery

import (
	"net"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/thediveo/clippy/cliplugin"
	"github.com/thediveo/go-plugger/v3"

	"github.com/siemens/packetflix"
)

// Names of the CLI flags provided in this package.
const (
	ServiceFlag = "discovery-service"
	PortFlag    = "gw-port"
	ProxyFlag   = "proxy-discovery"
)

func init() {
	plugger.Group[cliplugin.SetupCLI]().Register(
		setupCLI, plugger.WithPlugin("discovery"))
}

// setupCLI adds the discovery service flags.
func setupCLI(cmd *cobra.Command) {
	pf := cmd.PersistentFlags()
	pf.String(ServiceFlag, packetflix.DefaultDiscoveryService,
		"name/address of discovery service")
	pf.Uint16(PortFlag, packetflix.DefaultDiscoveryServicePort,
		"port of local Ghostwire discovery service")
	pf.Bool(ProxyFlag, false,
		"enable proxying discovery service SPA and API")
}

// Address returns the "host:port" address of the discovery service specified
// for the passed command.
func Address(cmd *cobra.Command) string {
	service, _ := cmd.Flags().GetString(ServiceFlag)
	port, _ := cmd.Flags().GetUint16(PortFlag)
	return net.JoinHostPort(service, strconv.Itoa(int(port)))
}

// IsProxying returns true if requests not handled otherwise should be reverse
// proxied to the discovery service.
func IsProxying(cmd *cobra.Command) bool {
	proxy, _ := cmd.Flags().GetBool(ProxyFlag)
	return proxy
}
