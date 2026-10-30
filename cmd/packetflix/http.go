// (c) Siemens AG 2023
//
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/thediveo/nonstd/xslog"

	"github.com/siemens/packetflix"
)

// versionHandler creates a /version JSON response giving versioning
// information about this micro service.
func versionHandler(w http.ResponseWriter, req *http.Request) {
	v := map[string]any{
		"name":    "packetflix",
		"version": buildVersion(),
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
// of requests is enabled, otherwise it directly returns the (de)muxer instead.
func requestLogger(h http.Handler, enabled bool) http.Handler {
	if !enabled {
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

// newDemux returns the demultiplexer for our HTTP API endpoints, optionally
// forwarding everything else to the discovery service at the specified
// "host:port" address.
//
// For unknown reasons the "thing" reponsible for demultiplexing incomming
// HTTP requests onto HTTP handlers is termed a "multiplexer". Yet, this is a
// demultiplexer. Confusing. See: https://en.wikipedia.org/wiki/Multiplexer
func newDemux(discovery string, proxyDiscovery bool) *http.ServeMux {
	demux := http.NewServeMux()
	demux.Handle("/capture", packetflix.NewCaptureHandler(discovery))
	demux.HandleFunc("/version", versionHandler)
	if proxyDiscovery {
		// Enable reverse proxying to the associated Ghostwire discovery service
		// instance at (fixed) path "/" ... everything not handled otherwise
		// will thus end up in the Ghostwire service and its SPA.
		slog.Debug("forwarding to discovery service enabled")
		demux.Handle("/", packetflix.NewDiscoveryProxy(discovery))
	}
	return demux
}
