package gorbital_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/gorbital"
	"gorbital.dev/modules/openapi"
)

// BenchmarkRequest compares a signed-in GET through a route registered by
// gorbital (with the authenticated-actor check) with the same operation
// registered directly on Huma, the budget in docs/benchmarks.md.
func BenchmarkRequest(b *testing.B) {
	direct := newTestAPI(b, withBearer())
	huma.Register(direct.api, huma.Operation{
		OperationID: "get-book", Method: http.MethodGet, Path: "/v1/books/{id}", Security: openapi.Bearer,
	}, getBook)

	composed := newTestAPI(b, withBearer())
	err := gorbital.Mount(composed.api, composed.mapper, gorbital.Deps{}, gorbital.Module{
		Name:   "books",
		Routes: func(r *gorbital.Router, _ gorbital.Deps) { gorbital.Get(r, "/v1/books/{id}", getBook) },
	})
	if err != nil {
		b.Fatal(err)
	}

	for _, bc := range []struct {
		name    string
		handler http.Handler
	}{
		{"huma", signedIn(direct.mux)},
		{"gorbital", signedIn(composed.mux)},
	} {
		b.Run(bc.name, func(b *testing.B) {
			req := httptest.NewRequest(http.MethodGet, "/v1/books/bok_1", nil)
			b.ReportAllocs()
			for b.Loop() {
				rec := httptest.NewRecorder()
				bc.handler.ServeHTTP(rec, req)
				if rec.Code != http.StatusOK {
					b.Fatalf("status %d", rec.Code)
				}
			}
		})
	}
}
