// (c) Siemens AG 2023
//
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("discovery service proxy", func() {

	var proxied chan *http.Request

	BeforeEach(func() {
		proxied = make(chan *http.Request, 1)
		fakeDiscovery(func(w http.ResponseWriter, req *http.Request) {
			proxied <- req.Clone(context.Background())
			w.WriteHeader(http.StatusNoContent)
		})
	})

	proxy := func(req *http.Request) *http.Request {
		GinkgoHelper()
		rec := httptest.NewRecorder()
		gwHandler.ServeHTTP(rec, req)
		Expect(rec.Code).To(Equal(http.StatusNoContent))
		var outreq *http.Request
		Expect(proxied).To(Receive(&outreq))
		return outreq
	}

	It("forwards requests to the discovery service", func() {
		outreq := proxy(httptest.NewRequest(http.MethodGet, "/foo/bar?baz=42", nil))
		Expect(outreq.URL.Path).To(Equal("/foo/bar"))
		Expect(outreq.URL.RawQuery).To(Equal("baz=42"))
		Expect(outreq.Host).To(Equal(fmt.Sprintf("%s:%d", DiscoveryService, DiscoveryPort)))
	})

	It("maps the legacy discovery API path", func() {
		Expect(proxy(httptest.NewRequest(http.MethodGet, "/discover/mobyshark", nil)).URL.Path).
			To(Equal("/mobyshark"))
	})

	It("only cleans request paths", func() {
		Expect(proxy(httptest.NewRequest(http.MethodGet, "/foo//bar/../baz/", nil)).URL.Path).
			To(Equal("/foo/baz"))
	})

	It("passes on the forwarding headers spaserve relies on", func() {
		req := httptest.NewRequest(http.MethodGet, "/foo", nil)
		req.Header.Set("X-Forwarded-Prefix", "/packetflix")
		req.Header.Set("X-Forwarded-Uri", "/packetflix/foo")
		outreq := proxy(req)
		Expect(outreq.Header.Get("X-Forwarded-Prefix")).To(Equal("/packetflix"))
		Expect(outreq.Header.Get("X-Forwarded-Uri")).To(Equal("/packetflix/foo"))
	})

	It("replaces client-supplied forwarding information", func() {
		req := httptest.NewRequest(http.MethodGet, "/foo", nil)
		req.Header.Set("X-Forwarded-For", "6.6.6.6")
		outreq := proxy(req)
		Expect(outreq.Header.Get("X-Forwarded-For")).To(Equal("192.0.2.1"))
		Expect(outreq.Header.Get("X-Forwarded-Host")).To(Equal("example.com"))
		Expect(outreq.Header.Get("X-Forwarded-Proto")).To(Equal("http"))
	})

	It("signals Ghostwire to enable its capture UI", func() {
		Expect(proxy(httptest.NewRequest(http.MethodGet, "/", nil)).Header).
			To(HaveKey(CaptureEnableHeader))
	})

})
