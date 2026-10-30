// (c) Siemens AG 2023
//
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"

	"github.com/onsi/gomega/gbytes"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("HTTP frontend", func() {

	It("reports its version", func() {
		rec := httptest.NewRecorder()
		versionHandler(rec, httptest.NewRequest(http.MethodGet, "/version", nil))
		Expect(rec.Body.String()).To(MatchJSON(
			fmt.Sprintf(`{"name":"packetflix","version":%q}`, buildVersion())))
	})

	When("demultiplexing", func() {

		It("serves the capture service API", func() {
			rec := httptest.NewRecorder()
			newDemux("", false).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/version", nil))
			Expect(rec.Code).To(Equal(http.StatusOK))
		})

		It("doesn't serve anything else unless proxying to the discovery service", func() {
			rec := httptest.NewRecorder()
			newDemux("", false).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
			Expect(rec.Code).To(Equal(http.StatusNotFound))
		})

	})

	When("logging requests", func() {

		It("doesn't sit in front of the handler unless enabled", func() {
			mux := http.NewServeMux()
			Expect(requestLogger(mux, false)).To(BeIdenticalTo(mux))
		})

		It("logs requests before handing them on", func() {
			logbuff := gbytes.NewBuffer()
			slog.SetDefault(slog.New(slog.NewTextHandler(logbuff, nil)))

			var handled bool
			h := requestLogger(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				handled = true
			}), true)
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/foo", nil))
			Expect(handled).To(BeTrue())
			Expect(logbuff).To(gbytes.Say(`msg=request remote=192\.0\.2\.1 method=GET path=/foo `))
		})

	})

})
