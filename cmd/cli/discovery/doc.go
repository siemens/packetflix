/*
Package discovery provides the “--discovery-service”, “--gw-port”, and
“--proxy-discovery” CLI flags for contacting the Ghostwire discovery service
and optionally reverse proxying requests to it.

The flags are automatically added to the root command as persistent flags. The
command then gets the discovery service address and checks whether to reverse
proxy to the discovery service by calling [Address] and [IsProxying]:

	addr := discovery.Address(cmd)
	proxy := discovery.IsProxying(cmd)
*/
package discovery
