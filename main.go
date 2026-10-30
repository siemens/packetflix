// (c) Siemens AG 2023
//
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"strings"

	"github.com/integrii/flaggy"
	"github.com/thediveo/nonstd/xslog"
)

// versionPage creates a /version JSON response giving versioning
// information about this micro service.
func versionHandler(w http.ResponseWriter, req *http.Request) {
	v := map[string]any{
		"name":    "packetflix",
		"version": SemVersion,
	}
	j, err := json.Marshal(v)
	if err != nil {
		slog.Error("cannot create version JSON", xslog.Error(err))
	}
	_, err = w.Write(j)
	if err != nil {
		slog.Error("cannot create /version", xslog.Error(err))
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
		slog.Info("request",
			slog.String("remote", addr),
			slog.String("method", req.Method),
			slog.String("path", req.URL.Path),
			slog.String("proto", req.Proto),
			slog.String("referer", req.Referer()),
			slog.String("user-agent", req.UserAgent()))
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

	logLevel := new(slog.LevelVar)
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})))

	// Get the show going...
	slog.Info("Packetflix Capture-as-a-Service", slog.String("version", SemVersion))

	// Set up CLI arg parsing.
	flaggy.SetName("Packetflix")
	flaggy.SetDescription("live network packet capture streaming from untouchable containers")
	flaggy.SetVersion(SemVersion)

	flaggy.Bool(&Debug, "", "debug", "log debugging messages")
	flaggy.Bool(&LogRequests, "", "log-requests", "log frontend HTTP/WS requests")
	flaggy.Bool(&LogRequestHeaders, "", "log-headers", "log frontend HTTP/WS request headers")

	flaggy.UInt16(&Port, "p", "port",
		"port to expose capture service on")
	flaggy.String(&DiscoveryService, "", "discovery-service",
		"name/address of discovery service")
	flaggy.UInt16(&DiscoveryPort, "", "gw-port",
		"port of local Ghostwire discovery service")
	flaggy.Bool(&ProxyDiscoveryService, "", "proxy-discovery",
		fmt.Sprintf("enable/disable proxy discovery service SPA and API (default: %t)", ProxyDiscoveryService))

	flaggy.Parse()

	if Debug {
		logLevel.Set(slog.LevelDebug)
		slog.Debug("debugging messages enabled")
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
		// Enable reverse proxying to the associated Ghostwire discovery service
		// instance at (fixed) path "/" ... everything not handled otherwise
		// will thus end up in the Ghostwire service and its SPA.
		slog.Debug("forwarding to discovery service enabled")
		demux.Handle("/", gwHandler)
	}
	slog.Info("starting capture service websocket server", slog.Int("port", int(Port)))
	err := http.ListenAndServe(
		fmt.Sprintf("[::]:%d", Port),
		requestLogger(demux))
	slog.Error("capture service terminated", xslog.Error(err))
}
