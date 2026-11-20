// (c) Siemens AG 2023
//
// SPDX-License-Identifier: MIT

package rootcmd

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
	"github.com/thediveo/nonstd/xslog"

	"github.com/siemens/packetflix"
	"github.com/siemens/packetflix/cmd/cli/discovery"
	"github.com/siemens/packetflix/cmd/cli/requestlog"
)

// NewVersionHandler returns a handler for the “/version” endpoint, responding
// with a JSON document giving the name and version of the passed command's
// root command.
func NewVersionHandler(cmd *cobra.Command) http.Handler {
	return versionHandler(cmd.Root().Name(), cmd.Root().Version)
}

// versionHandler returns a handler creating a /version JSON response giving
// versioning information about this micro service.
func versionHandler(name, version string) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		v := map[string]any{
			"name":    name,
			"version": version,
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
}

// LogRequests returns a handler that logs all HTTP and websocket requests
// before handing them to the passed handler, if enabled by the passed
// command's “--log-requests” or “--log-headers” CLI flags. Otherwise, it
// returns the passed handler unchanged.
func LogRequests(cmd *cobra.Command, h http.Handler) http.Handler {
	return requestLogger(h, requestlog.IsLogging(cmd))
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

// NewMux returns the demultiplexer for the stock packetflix endpoints
// “/capture” and “/version”, as well as for “/” forwarding everything else to
// the discovery service if enabled by the passed command's “--proxy-discovery”
// CLI flag.
//
// For unknown reasons the "thing" reponsible for demultiplexing incomming
// HTTP requests onto HTTP handlers is termed a "multiplexer". Yet, this is a
// demultiplexer. Confusing. See: https://en.wikipedia.org/wiki/Multiplexer
func NewMux(cmd *cobra.Command) *http.ServeMux {
	discoveryAddr := discovery.Address(cmd)
	demux := http.NewServeMux()
	demux.Handle("/capture", packetflix.NewCaptureHandler(discoveryAddr))
	demux.Handle("/version", NewVersionHandler(cmd))
	if discovery.IsProxying(cmd) {
		// Enable reverse proxying to the associated Ghostwire discovery service
		// instance at (fixed) path "/" ... everything not handled otherwise
		// will thus end up in the Ghostwire service and its SPA.
		slog.Debug("forwarding to discovery service enabled")
		demux.Handle("/", packetflix.NewDiscoveryProxy(discoveryAddr))
	}
	return demux
}
