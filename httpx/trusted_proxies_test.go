package httpx_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"gorbital.dev/httpx"
)

func TestParseTrustedProxies(t *testing.T) {
	got, err := httpx.ParseTrustedProxies(" 10.0.0.0/8, 192.0.2.10 ,2001:db8::/32,")
	if err != nil || len(got) != 3 || got[1].String() != "192.0.2.10/32" {
		t.Fatalf("ParseTrustedProxies() = %v, %v", got, err)
	}
	if got, err := httpx.ParseTrustedProxies(""); err != nil || len(got) != 0 {
		t.Errorf("ParseTrustedProxies(empty) = %v, %v", got, err)
	}
	for _, all := range []string{"0.0.0.0/0", "::/0", "10.0.0.0/8,0.0.0.0/0"} {
		if _, err := httpx.ParseTrustedProxies(all); !errors.Is(err, httpx.ErrTrustAll) {
			t.Errorf("ParseTrustedProxies(%q) error = %v, want ErrTrustAll", all, err)
		}
	}
	if _, err := httpx.ParseTrustedProxies("load-balancer"); err == nil {
		t.Error("ParseTrustedProxies(host name) error = nil")
	}
}

func TestTrustedProxies(t *testing.T) {
	trusted, err := httpx.ParseTrustedProxies("10.0.0.0/8,2001:db8::/32")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, peer string
		forwarded  []string
		want       string
	}{
		{"direct client", "203.0.113.7:4242", nil, "203.0.113.7:4242"},
		{"untrusted peer can't spoof", "203.0.113.7:4242", []string{"198.51.100.1"}, "203.0.113.7:4242"},
		{"one trusted proxy", "10.0.0.5:80", []string{"198.51.100.1"}, "198.51.100.1:0"},
		{"client-supplied hops left of the proxy are ignored", "10.0.0.5:80", []string{"1.2.3.4, 198.51.100.1"}, "198.51.100.1:0"},
		{"several trusted proxies", "10.0.0.5:80", []string{"198.51.100.1, 10.1.1.1", "10.2.2.2"}, "198.51.100.1:0"},
		{"IPv6 client through an IPv6 proxy", "[2001:db8::1]:443", []string{"2001:db8:ffff::9, 2001:db9::7"}, "[2001:db9::7]:0"},
		{"IPv4-mapped peer", "[::ffff:10.0.0.5]:80", []string{"198.51.100.1"}, "198.51.100.1:0"},
		{"only trusted hops", "10.0.0.5:80", []string{"10.3.3.3"}, "10.0.0.5:80"},
		{"no header from a trusted proxy", "10.0.0.5:80", nil, "10.0.0.5:80"},
		{"malformed hop", "10.0.0.5:80", []string{"not-an-ip"}, "10.0.0.5:80"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			h := httpx.TrustedProxies(trusted)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r.RemoteAddr }))
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.peer
			for _, f := range tt.forwarded {
				req.Header.Add("X-Forwarded-For", f)
			}
			h.ServeHTTP(httptest.NewRecorder(), req)
			if got != tt.want {
				t.Errorf("RemoteAddr = %q, want %q", got, tt.want)
			}
		})
	}

	// Without trusted ranges the handler is unchanged.
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr, req.Header["X-Forwarded-For"] = "10.0.0.5:80", []string{"198.51.100.1"}
	var got string
	httpx.TrustedProxies(nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r.RemoteAddr })).ServeHTTP(httptest.NewRecorder(), req)
	if got != "10.0.0.5:80" {
		t.Errorf("RemoteAddr without trusted proxies = %q", got)
	}
	_ = next
}
