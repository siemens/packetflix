// (c) Siemens AG 2023
//
// SPDX-License-Identifier: MIT

// Packetflix is a capture-as-a-service implementation: it captures network
// packets from the network interfaces of a container, pod, process-less virtual
// IP stack, et cetera. The captured network packets are then streamed live via
// a websocket connection to clients connecting to our service.
//
// If available, then Packetflix will use GhostWire to update stale container
// references with the most up-to-date information as to capture correctly from
// pods and containers which were reloaded, et cetera. For this, run GhostWire
// side-by-side with Packetflix.
package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/integrii/flaggy"
	log "github.com/sirupsen/logrus"
)

// versionPage creates a /version JSON response giving versioning
// information about this micro service.
func versionHandler(w http.ResponseWriter, req *http.Request) {
	v := map[string]interface{}{
		"name":    "packetflix",
		"version": SemVersion,
	}
	j, err := json.Marshal(v)
	if err != nil {
		log.Errorf("cannot create version JSON: %s", err.Error())
	}
	_, err = w.Write(j)
	if err != nil {
		log.Errorf("cannot create /version: %s", err.Error())
	}
}

// A HTTP request logging handler that sits in front of the (de)mux so it sees
// all HTTP/WS requests and can log them before handing them to the (de)muxer.
// It only puts itself in front of the specified (de)mux HTTP handler if logging
// of requests (or headers) is enabled, otherwise it directly returns the
// (de)muxer instead.
func requestLogger(h http.Handler) http.Handler {
	if !LogRequests && !LogRequestHeaders {
		return h
	}
	// Prepend the (de)mux handler with the real logging HTTP handler...
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		addr := req.RemoteAddr
		if i := strings.LastIndex(addr, ":"); i != -1 {
			addr = addr[:i]
		}
		log.Infof("%s - - [%s] %q %d %d %q %q",
			addr,
			time.Now().Format("02/Jan/2006:15:04:05 -0700"),
			fmt.Sprintf("%s %s %s", req.Method, req.URL.Path, req.Proto),
			-1,
			-1,
			req.Referer(),
			req.UserAgent())
		// Hand over to the (de)muxer handler...
		h.ServeHTTP(w, req)
	})
}

// The drama unfolds...
func main() {
	// Not strictly necessary, but makes life easier for lxkns/Ghostwire:
	// without it, we might end up with the situation where the OS-level thread
	// from which we started the dumpcap binary and which we thus switched into
	// the target's network namespace ends up being the main thread and thus the
	// process itself. Now that then makes lxkns see packetflix' process being
	// the ealdorman in a network namespace different from its container's
	// network namespace.
	runtime.LockOSThread()

	// Seed the random number generator from the current time ... this is
	// perfectly acceptable in our case since we won't do any crypto dances
	// lateron, so this miserable randomness will suffice: we only need it to
	// generate random capture stream "pet names" which aren't the same sequence
	// on any restart.
	rand.Seed(time.Now().UTC().UnixNano())

	log.SetFormatter(&log.TextFormatter{
		ForceColors:   true,
		FullTimestamp: true,
	})

	// Get the show going...
	log.Infof("Packetflix Capture-as-a-Service version %s", SemVersion)

	// Set up CLI arg parsing.
	flaggy.SetName("Packetflix")
	flaggy.SetDescription("live network packet capture streaming from untouchable containers")
	flaggy.SetVersion(SemVersion)

	flaggy.Bool(&Debug, "", "debug", "log debugging messages")
	flaggy.Bool(&LogRequests, "", "log-requests", "log frontend HTTP/WS requests")
	flaggy.Bool(&LogRequestHeaders, "", "log-headers", "log frontend HTTP/WS request headers")

	flaggy.UInt16(&Port, "p", "port",
		fmt.Sprintf("port to expose capture service on (default: %d)", Port))
	flaggy.String(&DiscoveryService, "", "discovery-service",
		fmt.Sprintf("name/address of discovery service (default: %q)", DiscoveryService))
	flaggy.UInt16(&DiscoveryPort, "", "gw-port",
		fmt.Sprintf("port of local GhostWire discovery service (default: %d)", DiscoveryPort))
	flaggy.Bool(&ProxyDiscoveryService, "", "proxy-discovery",
		fmt.Sprintf("enable/disable proxy discovery service SPA and API (default: %t)", ProxyDiscoveryService))

	flaggy.Parse()

	if Debug {
		log.SetLevel(log.DebugLevel)
		log.Debug("debugging messages enabled")
	}

	// For unknown reasons the "thing" reponsible for demultiplexing incomming
	// HTTP requests onto HTTP handlers is termaed a "multiplexer". Yet, this is
	// a demultiplexer. Confusing.
	//
	// See: https://en.wikipedia.org/wiki/Multiplexer
	demux := http.NewServeMux()
	demux.HandleFunc("/capture", captureHandler)
	demux.HandleFunc("/version", versionHandler)
	if ProxyDiscoveryService {
		// Enable reverse proxying to the associated GhostWire discovery service
		// instance at (fixed) path "/" ... everything not handled otherwise
		// will thus end up in the Ghostwire service and its SPA.
		log.Debug("forwarding to discovery service enabled")
		demux.Handle("/", gwHandler)
	}
	log.Infof("starting capture service websocket server on port %d...", Port)
	log.Error(
		http.ListenAndServe(
			fmt.Sprintf("[::]:%d", Port),
			requestLogger(demux)))
}
