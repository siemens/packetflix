// (c) Siemens AG 2023
//
// SPDX-License-Identifier: MIT

// In case forwarding any non-capture API-related requests is enabled, then this
// handler reverse proxies everthing not caught otherwise to the ghostwire
// service. This is necessary in order to support the mark II user interface of
// Ghostwire, which is a React-based SPA (single page application).

package main

import (
	"fmt"
	"log/slog"
	"net/http/httputil"
	"net/url"
	"path"
)

// Handle the /discover route: it gets forwarded into the associated discovery
// service HTTP endpoint. Includes dreaded URL path rewriting.
//
// Contract with Ghostwire: its React SPA is served via
// github.com/thediveo/spaserve, which derives the SPA's HTML base path from the
// X-Forwarded-Prefix and X-Forwarded-Uri request headers set by upstream
// proxies, comparing them against the request path it receives. The SPA then
// builds its "packetflix:" capture URLs as <base>capture and its websocket URLs
// as <base>mobydig, so its base must map onto our root. Thus, this proxy MUST:
//   - pass X-Forwarded-Prefix and X-Forwarded-Uri through unmodified (Rewrite
//     only strips Forwarded and X-Forwarded-{For,Host,Proto}, which Ghostwire
//     doesn't use);
//   - forward the request path unchanged except for path.Clean normalization
//     (spaserve cleans it the same way before deriving the base), or else set
//     X-Forwarded-Prefix to whatever path prefix it strips;
//   - set the CaptureEnableHeader (Ghostwire only checks its presence) so the
//     SPA shows its capture buttons.
//
// Breaking this makes the SPA load its assets from wrong URLs in path-rewriting
// deployments, or makes its capture links miss our /capture endpoint.
var gwHandler = &httputil.ReverseProxy{
	// The rewriter rewrites the original request URL in order to now address
	// the discovery service. We have to do path rewriting here, which will
	// roughly work, as the discovery service Ghostwire has been developed with
	// path rewriting proxies in mind.
	//
	// NOTE: since this is a request received on the server side, Request.URL
	// will *NOT* contain any scheme and host information! Also, we expect our
	// handler to be called with any path prefix having already been removed and
	// the URL now is relative!
	Rewrite: func(pr *httputil.ProxyRequest) {
		req := pr.Out
		slog.Debug("handling request", "path", req.URL.Path)

		// Handle the old plugin discovery API path, too.
		if req.URL.Path == "/discover/mobyshark" {
			req.URL.Path = "/mobyshark"
		}

		req.URL.Path = path.Join("/", req.URL.Path)
		pr.SetURL(&url.URL{
			Scheme: "http",
			Host:   fmt.Sprintf("%s:%d", DiscoveryService, DiscoveryPort),
		})
		// Keeps X-Forwarded-{Prefix,Uri} intact, see the Ghostwire contract above.
		pr.SetXForwarded()
		// Signal the Ghostwire SPA to enable capture functionality in its UI.
		req.Header.Set(CaptureEnableHeader, "Affirmative, Dave")
		slog.Debug("forwarding/reverse proxying", "url", req.URL.String())
	},
}
