// (c) Siemens AG 2023
//
// SPDX-License-Identifier: MIT

/*
Packetflix is a capture-as-a-service implementation: it captures network
packets from the network interfaces of a container, pod, process-less virtual
IP stack, et cetera. The captured network packets are then streamed live via
a websocket connection to clients connecting to our service.

If available, then Packetflix will use Ghostwire to update stale container
references with the most up-to-date information as to capture correctly from
pods and containers which were reloaded, et cetera. For this, run Ghostwire
side-by-side with Packetflix.
*/
package main
