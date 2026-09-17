package httpx_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"testing"

	"gorbital.dev/httpx"
	"gorbital.dev/httpx/ipfilter"
)

func TestParseTrustedProxies(t *testing.T) {
	got, err := httpx.ParseTrustedProxies(" 10.0.0.0/8, 192.0.2.10 ,2001:db8::/32,")
	if err != nil || len(got) != 3 || got[1].String() != "192.0.2.10/32" {
		t.Fatalf("ParseTrustedProxies() = %v, %v", got, err)
	}
	if got, err := httpx.ParseTrustedProxies(""); err != nil || len(got) != 0 {
		t.Errorf("ParseTrustedProxies(empty) = %v, %v", got, err)
	}
	for _, all := range []string{"0.0.0.0/0", "::/0", "10.0.0.0/8,0.0.0.0/0", "::ffff:0.0.0.0/96", "::ffff:0:0/96"} {
		if _, err := httpx.ParseTrustedProxies(all); !errors.Is(err, httpx.ErrTrustAll) {
			t.Errorf("ParseTrustedProxies(%q) error = %v, want ErrTrustAll", all, err)
		}
	}
	if _, err := httpx.ParseTrustedProxies("load-balancer"); err == nil {
		t.Error("ParseTrustedProxies(host name) error = nil")
	}

	// IPv4-mapped entries become IPv4, as the compared addresses are
	// (ipfilter.ParsePrefixes does the same).
	for _, tt := range []struct{ list, want string }{
		{"::ffff:10.0.0.5", "10.0.0.5/32"},
		{"::ffff:10.0.0.5/128", "10.0.0.5/32"},
		{"::ffff:10.0.0.0/104", "10.0.0.0/8"},
	} {
		got, err := httpx.ParseTrustedProxies(tt.list)
		if err != nil || len(got) != 1 || got[0].String() != tt.want {
			t.Errorf("ParseTrustedProxies(%q) = %v, %v, want [%s]", tt.list, got, err, tt.want)
		}
	}
	// A mapped range shorter than /96 mixes families and could never match.
	for _, mixed := range []string{"::ffff:0:0/80", "::ffff:10.0.0.0/64"} {
		if _, err := httpx.ParseTrustedProxies(mixed); err == nil || errors.Is(err, httpx.ErrTrustAll) {
			t.Errorf("ParseTrustedProxies(%q) error = %v, want a mixed-family error", mixed, err)
		}
	}
	// A zoned address could never match a peer address either.
	for _, zoned := range []string{"fe80::1%en0", "fe80::1%en0/64"} {
		if _, err := httpx.ParseTrustedProxies(zoned); err == nil {
			t.Errorf("ParseTrustedProxies(%q) error = nil", zoned)
		}
	}
}

// TestParseTrustedProxiesMatchesIPFilter keeps ParseTrustedProxies and
// ipfilter.ParsePrefixes canonicalising the same way; both compare
// unmapped, unzoned client addresses. The lists below are the ones the two
// packages both accept or both refuse.
func TestParseTrustedProxiesMatchesIPFilter(t *testing.T) {
	for _, list := range []string{
		"10.0.0.5/8", "192.0.2.10", "2001:db8::1/32", "::ffff:10.0.0.5", "::ffff:10.0.0.0/104",
		"::ffff:0:0/80", "fe80::1%en0", "fe80::/10", "load-balancer", "192.0.2.1/33", "",
		" 10.0.0.0/8, ::ffff:192.0.2.10 ,2001:db8::/32,",
	} {
		proxies, proxyErr := httpx.ParseTrustedProxies(list)
		allowed, allowErr := ipfilter.ParsePrefixes(list)
		if (proxyErr != nil) != (allowErr != nil) {
			t.Errorf("ParseTrustedProxies(%q) error = %v; ParsePrefixes error = %v", list, proxyErr, allowErr)
			continue
		}
		if !slices.Equal(proxies, allowed) {
			t.Errorf("ParseTrustedProxies(%q) = %v; ParsePrefixes = %v", list, proxies, allowed)
		}
	}
}

func TestTrustedProxies(t *testing.T) {
	trusted, err := httpx.ParseTrustedProxies("10.0.0.0/8,2001:db8::/32")
	if err != nil {
		t.Fatal(err)
	}
	// The same proxies written as IPv4-mapped IPv6, which an operator may
	// copy from a log line. They must trust exactly the same peers.
	mapped, err := httpx.ParseTrustedProxies("::ffff:10.0.0.0/104,2001:db8::/32")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, peer string
		forwarded  []string
		want       string
		trusted    []netip.Prefix // nil: trusted
	}{
		{"direct client", "203.0.113.7:4242", nil, "203.0.113.7:4242", nil},
		{"untrusted peer can't spoof", "203.0.113.7:4242", []string{"198.51.100.1"}, "203.0.113.7:4242", nil},
		{"one trusted proxy", "10.0.0.5:80", []string{"198.51.100.1"}, "198.51.100.1:0", nil},
		{"client-supplied hops left of the proxy are ignored", "10.0.0.5:80", []string{"1.2.3.4, 198.51.100.1"}, "198.51.100.1:0", nil},
		{"several trusted proxies", "10.0.0.5:80", []string{"198.51.100.1, 10.1.1.1", "10.2.2.2"}, "198.51.100.1:0", nil},
		{"IPv6 client through an IPv6 proxy", "[2001:db8::1]:443", []string{"2001:db8:ffff::9, 2001:db9::7"}, "[2001:db9::7]:0", nil},
		{"IPv4-mapped peer", "[::ffff:10.0.0.5]:80", []string{"198.51.100.1"}, "198.51.100.1:0", nil},
		{"only trusted hops", "10.0.0.5:80", []string{"10.3.3.3"}, "10.0.0.5:80", nil},
		{"no header from a trusted proxy", "10.0.0.5:80", nil, "10.0.0.5:80", nil},
		{"malformed hop", "10.0.0.5:80", []string{"not-an-ip"}, "10.0.0.5:80", nil},
		// An IPv4-mapped *entry* now matches, mapped peer or not: before
		// canonicalisation it stayed a /128 mapped range and matched nothing.
		{"IPv4-mapped entry, IPv4 peer", "10.0.0.5:80", []string{"198.51.100.1"}, "198.51.100.1:0", mapped},
		{"IPv4-mapped entry, mapped peer", "[::ffff:10.0.0.5]:80", []string{"198.51.100.1"}, "198.51.100.1:0", mapped},
		{"IPv4-mapped entry, untrusted peer", "203.0.113.7:4242", []string{"198.51.100.1"}, "203.0.113.7:4242", mapped},
		{"IPv4-mapped entry, mapped hop", "10.0.0.5:80", []string{"::ffff:198.51.100.1"}, "198.51.100.1:0", mapped},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ranges := tt.trusted
			if ranges == nil {
				ranges = trusted
			}
			var got string
			h := httpx.TrustedProxies(ranges)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r.RemoteAddr }))
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
func TestTrustedProxiesDropsRangesItCantUse(t *testing.T) {
	mapped := netip.MustParsePrefix("::ffff:10.0.0.0/104")
	mixed := netip.MustParsePrefix("::ffff:10.0.0.0/8") // masks to ::/8
	for name, tt := range map[string]struct {
		trusted []netip.Prefix
		peer    string
		want    string
	}{
		"a mapped range still trusts its proxy": {[]netip.Prefix{mapped}, "10.0.0.5:80", "198.51.100.1:0"},
		"a family-mixing range trusts nothing":  {[]netip.Prefix{mixed}, "[::1]:80", "[::1]:80"},
		"a family-mixing range isn't re-based":  {[]netip.Prefix{mixed}, "10.0.0.5:80", "10.0.0.5:80"},
	} {
		t.Run(name, func(t *testing.T) {
			var got string
			h := httpx.TrustedProxies(tt.trusted)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r.RemoteAddr }))
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.peer
			req.Header.Set("X-Forwarded-For", "198.51.100.1")
			h.ServeHTTP(httptest.NewRecorder(), req)
			if got != tt.want {
				t.Errorf("RemoteAddr = %q, want %q", got, tt.want)
			}
		})
	}
}
