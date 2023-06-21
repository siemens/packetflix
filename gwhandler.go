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
	"net/http"
	"net/http/httputil"
	"path"

	log "github.com/sirupsen/logrus"
)

// Handle the /discover route: it gets forwarded into the associated discovery
// service HTTP endpoint. Includes dreaded URL path rewriting.
var gwHandler = &httputil.ReverseProxy{
	// The "director" (a.k.a. URL rewriter) rewrites the original request URL in
	// order to now address the discovery service. We have to do path rewriting
	// here, which will roughly work, as the discovery service GhostWire has
	// been developed with path rewriting proxies in mind.
	//
	// NOTE: since this is a request received on the server side, Request.URL
	// will *NOT* contain any scheme and host information! Also, we expect our
	// handler to be called with any path prefix having already been removed and
	// the URL now is relative!
	Director: func(req *http.Request) {
		log.Debugf("handling %q...", req.URL.Path)

		// Handle the old plugin discovery API path, too.
		if req.URL.Path == "/discover/mobyshark" {
			req.URL.Path = "/mobyshark"
		}

		req.URL.Host = fmt.Sprintf("%s:%d", DiscoveryService, DiscoveryPort)
		req.URL.Path = path.Join("/", req.URL.Path)
		// see https://github.com/golang/go/issues/28168 about having to change
		// req.Host also because it gets preferred ... and the stock
		// httputil.NewSingleHostReverseProxy is buggy. Oh ... well. :(
		req.Host = req.URL.Host
		req.URL.Scheme = "http"
		// See also httputil.NewSingleHostReverseProxy for handling the
		// User-Agent header.
		if _, ok := req.Header["User-Agent"]; !ok {
			req.Header.Set("User-Agent", "")
		}
		// Signal the Ghostwire SPA to enable capture functionality in its UI.
		req.Header.Set(CaptureEnableHeader, "Affirmative, Dave")
		log.Debugf("forwarding/reverse proxying to %q", req.URL.String())
	},
}
