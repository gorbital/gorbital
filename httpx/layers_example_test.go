package httpx_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	"gorbital.dev/httpx"
)

func ExampleTimeout() {
	slowReport := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(time.Second): // a query that takes too long
			_, _ = w.Write([]byte("report"))
		case <-r.Context().Done():
			// The query stops with the request's context.
		}
	})
	h := httpx.Chain(slowReport, httpx.RequestID(), httpx.Timeout(10*time.Millisecond))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/reports/1", nil))
	fmt.Println(rec.Code, problemCode(rec.Body.Bytes()))
	// Output: 503 request_timeout
}

func ExampleTimeout_deadline() {
	// Handlers read the deadline from the request's context and pass the
	// context on, so queries stop in time.
	h := httpx.Timeout(30 * time.Second)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deadline, ok := r.Context().Deadline()
		fmt.Println(ok, time.Until(deadline) > 29*time.Second)
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second) // a shorter step inside
		defer cancel()
		_ = ctx
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	// Output: true true
}

func ExampleIPFilter() {
	allow, err := httpx.ParsePrefixes("10.0.0.0/8, 2001:db8::/32")
	if err != nil {
		panic(err)
	}
	onlyOffice, err := httpx.IPFilter(allow, nil)
	if err != nil {
		panic(err)
	}
	ops := httpx.Chain(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}), onlyOffice)

	for _, addr := range []string{"10.1.2.3:52000", "203.0.113.9:52000"} {
		req := httptest.NewRequest(http.MethodGet, "/ops/system", nil)
		req.RemoteAddr = addr
		rec := httptest.NewRecorder()
		ops.ServeHTTP(rec, req)
		fmt.Println(addr, rec.Code, rec.Body.String() == "ok" || problemCode(rec.Body.Bytes()) == "ip_not_allowed")
	}
	// Output:
	// 10.1.2.3:52000 200 true
	// 203.0.113.9:52000 403 true
}

func ExampleIPFilter_behindAProxy() {
	// Behind a load balancer, resolve the client address first.
	proxies, _ := httpx.ParseTrustedProxies("10.0.0.0/8")
	deny, _ := httpx.ParsePrefixes("198.51.100.0/24")
	filter, err := httpx.IPFilter(nil, deny)
	if err != nil {
		panic(err)
	}
	h := httpx.Chain(http.NotFoundHandler(), httpx.TrustedProxies(proxies), filter)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.2:443"
	req.Header.Set("X-Forwarded-For", "198.51.100.7")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	fmt.Println(rec.Code)
	// Output: 403
}

func ExampleParsePrefixes() {
	p, err := httpx.ParsePrefixes("10.0.0.5/8, 192.0.2.10, ::ffff:198.51.100.1")
	fmt.Println(p, err)
	_, err = httpx.ParsePrefixes("office-vpn")
	fmt.Println(err)
	// Output:
	// [10.0.0.0/8 192.0.2.10/32 198.51.100.1/32] <nil>
	// httpx: "office-vpn" is not a CIDR range or IP address
}

// problemCode returns the code of a problem response body.
func problemCode(body []byte) string {
	var p httpx.Problem
	_ = json.Unmarshal(body, &p)
	return p.Code
}
