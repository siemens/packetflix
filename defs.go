// (c) Siemens AG 2023
//
// SPDX-License-Identifier: MIT

package packetflix

import (
	"time"
)

const (
	// DefaultPacketFlixServicePort specifies the default service port number.
	DefaultPacketFlixServicePort = 5001

	// DefaultDiscoveryServicePort specifies the port number of the Ghostwire
	// service.
	DefaultDiscoveryServicePort = 5000

	// DefaultDiscoveryService specifies the (host) name or address of the
	// Ghostwire service.
	DefaultDiscoveryService = "127.0.0.1"

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
