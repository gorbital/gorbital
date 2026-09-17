package timeout_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	"gorbital.dev/httpx"
	"gorbital.dev/httpx/timeout"
)

func ExampleNew() {
	slowReport := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(time.Second): // a query that takes too long
			_, _ = w.Write([]byte("report"))
		case <-r.Context().Done():
			// The query stops with the request's context.
		}
	})
	h := httpx.Chain(slowReport, httpx.RequestID(), timeout.New(10*time.Millisecond))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/reports/1", nil))
	fmt.Println(rec.Code, problemCode(rec.Body.Bytes()))
	// Output: 503 request_timeout
}

func ExampleNew_deadline() {
	// Handlers read the deadline from the request's context and pass the
	// context on, so queries stop in time.
	h := timeout.New(30 * time.Second)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deadline, ok := r.Context().Deadline()
		fmt.Println(ok, time.Until(deadline) > 29*time.Second)
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second) // a shorter step inside
		defer cancel()
		_ = ctx
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	// Output: true true
}

// problemCode returns the code of a problem response body.
func problemCode(body []byte) string {
	var p httpx.Problem
	_ = json.Unmarshal(body, &p)
	return p.Code
}
