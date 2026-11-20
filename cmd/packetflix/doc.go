/*
Packetflix is a capture-as-a-service: it captures network packets from the
network interfaces of a container, pod, process-less virtual IP stack, et
cetera, and streams them live via websocket connections to its clients.

Run Ghostwire side-by-side with Packetflix in order to correctly capture from
pods and containers which were reloaded, et cetera. Optionally, Packetflix
reverse proxies everything not part of its own API to Ghostwire.

# Usage

	packetflix [flags]

# Flags

Capture service:

  - --port, -p uint16: port to expose capture service on (default 5001).

Ghostwire discovery service:

  - --discovery-service string: name/address of discovery service (default
    "127.0.0.1").
  - --gw-port uint16: port of local Ghostwire discovery service (default 5000).
  - --proxy-discovery: enable proxying discovery service SPA and API.

Logging:

  - --debug: enables debug structured logging output.
  - --tinted: tints structured logging output.
  - --log-requests: log frontend HTTP/WS requests.
  - --log-headers: log frontend HTTP/WS request headers; implies
    --log-requests.

Miscellaneous:

  - --version, -v: version for packetflix.
  - --help, -h: help for packetflix.

Use “packetflix --help” to get the list of CLI flags actually available.

# Downstream Customizations

Downstream customizations reuse the packetflix command from package
[github.com/siemens/packetflix/cmd/packetflix/rootcmd].
*/
package main
