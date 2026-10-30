/*
Packetflix is a capture-as-a-service: it captures network packets from the
network interfaces of a container, pod, process-less virtual IP stack, et
cetera, and streams them live via websocket connections to its clients.

Run Ghostwire side-by-side with Packetflix in order to correctly capture from
pods and containers which were reloaded, et cetera. Optionally, Packetflix
reverse proxies everything not part of its own API to Ghostwire.

Use “packetflix --help” to get a list of available CLI flags.
*/
package main
