// (c) Siemens AG 2023
//
// SPDX-License-Identifier: MIT

//go:generate go run ./internal/gen/version

package main

import (
	"time"
)

const (
	// DefaultPacketFlixServicePort specifies the default service port number.
	DefaultPacketFlixServicePort = 5001

	// DefaultDiscoveryServicePort specifies the port number of the GhostWire
	// service.
	DefaultDiscoveryServicePort = 5000

	// CaptureEnableHeader tells the Ghostwire service to serve its SPA user
	// interface with capture button enabled.
	CaptureEnableHeader = "Enable-Monolith"

	// DiscoveryDeadline specifies the maximum amout of time to wait for the
	// container discovery service to respond.
	DiscoveryDeadline = 20 * time.Second

	// ClosingDeadline specifies the maximum amount of time to wait for a full
	// (opt. graceful) closing procedure to take.
	ClosingDeadline = 10 * time.Second
)

const (
	// CaptureProgram is the location and name of the packet capture program.
	CaptureProgram = "/usr/bin/dumpcap"

	// StdErrCapturePrefix defines the prefix used by the capture program when
	// emitting error messages.
	StdErrCapturePrefix = "dumpcap: "
)

// Global settings, controllable through CLI arguments.
var (
	// Debug ("--debug") enables logging debug messages.
	Debug = false
	// Port ("--port" or just "-p") specifies the TCP port of our Packetflix
	// capture service.
	Port uint16 = DefaultPacketFlixServicePort
	// DiscoveryPort ("--gw-port") specifies the TCP port where to contact the
	// GhostWire discovery service locally within the same pod or host.
	DiscoveryPort uint16 = DefaultDiscoveryServicePort
	// (Host) Name of the discovery service, if not reachable locally via
	// 127.0.0.1.
	DiscoveryService string = "127.0.0.1"
	// ProxyDiscoveryService ("--proxy-discovery") switches on the "/discovery"
	// proxy route to the discovery service.
	ProxyDiscoveryService = false
	// LogRequests enables logging HTTP/WS requests to the frontend.
	LogRequests = false
	// LogRequestHeaders enables logging HTTP/WS request headers to the frontend.
	// Includes LogRequests.
	LogRequestHeaders = false
)
