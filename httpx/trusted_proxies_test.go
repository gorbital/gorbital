package httpx_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
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

// TestParseTrustedProxiesMappedEntries: an IPv4-mapped IPv6 entry names an
// IPv4 range, and is read as one, because the addresses it is compared with
// are unmapped. Before this, "::ffff:10.0.0.5" and "::ffff:10.0.0.0/104"
// were kept 4-in-6 and matched nothing at all -- not even a mapped peer --
// so an operator's list silently trusted nobody; and a mapped range shorter
// than /96 was re-based by netip onto a range they never wrote, so
// "::ffff:10.0.0.0/8" masked to "::/8" and trusted ::1 (internal security
// review, 2026-09, HTTP-1).
func TestParseTrustedProxiesMappedEntries(t *testing.T) {
	for entry, want := range map[string]string{
		"::ffff:10.0.0.5":      "10.0.0.5/32",
		"::ffff:10.0.0.0/104":  "10.0.0.0/8",
		"::ffff:192.0.2.0/120": "192.0.2.0/24",
	} {
		got, err := httpx.ParseTrustedProxies(entry)
		if err != nil || len(got) != 1 || got[0].String() != want {
			t.Errorf("ParseTrustedProxies(%q) = %v, %v; want [%s]", entry, got, err, want)
		}
	}
	// A mapped range shorter than /96 mixes families: refused, never re-based.
	for _, entry := range []string{"::ffff:10.0.0.0/8", "::ffff:0.0.0.0/64"} {
		if _, err := httpx.ParseTrustedProxies(entry); err == nil {
			t.Errorf("ParseTrustedProxies(%q) error = nil", entry)
		}
	}
	// "every IPv4 address", written the mapped way, is still ErrTrustAll.
	if _, err := httpx.ParseTrustedProxies("::ffff:0.0.0.0/96"); !errors.Is(err, httpx.ErrTrustAll) {
		t.Errorf("ParseTrustedProxies(mapped 0.0.0.0/0) error = %v, want ErrTrustAll", err)
	}
	// A zoned address is not a proxy range.
	if _, err := httpx.ParseTrustedProxies("fe80::1%eth0"); err == nil {
		t.Error("ParseTrustedProxies(zoned address) error = nil")
	}

	// End to end: a mapped entry trusts the proxy it names, whether the peer
	// arrives as IPv4 or IPv4-mapped.
	trusted, err := httpx.ParseTrustedProxies("::ffff:10.0.0.0/104")
	if err != nil {
		t.Fatal(err)
	}
	for _, peer := range []string{"10.0.0.5:80", "[::ffff:10.0.0.5]:80"} {
		var got string
		h := httpx.TrustedProxies(trusted)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r.RemoteAddr }))
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = peer
		req.Header.Set("X-Forwarded-For", "198.51.100.1")
		h.ServeHTTP(httptest.NewRecorder(), req)
		if got != "198.51.100.1:0" {
			t.Errorf("peer %s: RemoteAddr = %q, want the forwarded client", peer, got)
		}
	}
}

// TestTrustedProxiesDropsRangesItCantUse: prefixes passed straight to the
// middleware are canonicalised like parsed ones, and one that can't be is
// dropped rather than trusting a range nobody wrote.
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
