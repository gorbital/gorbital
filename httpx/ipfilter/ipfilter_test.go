package ipfilter_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strings"
	"testing"

	"gorbital.dev/httpx"
	"gorbital.dev/httpx/ipfilter"
)

func prefixes(t testing.TB, list string) []netip.Prefix {
	t.Helper()
	p, err := ipfilter.ParsePrefixes(list)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func problemCodeOf(t *testing.T, body []byte) string {
	t.Helper()
	var p struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(body, &p)
	return p.Code
}

func TestNew(t *testing.T) {
	tests := []struct {
		name       string
		allow      string
		deny       string
		remoteAddr string
		want       int
	}{
		{"no allow list allows", "", "203.0.113.0/24", "198.51.100.7:1234", 200},
		{"denied", "", "203.0.113.0/24", "203.0.113.9:1234", 403},
		{"allowed", "10.0.0.0/8, 192.0.2.10", "", "10.1.2.3:1234", 200},
		{"single address allowed", "10.0.0.0/8, 192.0.2.10", "", "192.0.2.10:443", 200},
		{"not in allow list", "10.0.0.0/8, 192.0.2.10", "", "192.0.2.11:443", 403},
		{"deny wins over allow", "10.0.0.0/8", "10.0.5.0/24", "10.0.5.1:1", 403},
		{"allowed next to a denied range", "10.0.0.0/8", "10.0.5.0/24", "10.0.6.1:1", 200},
		{"IPv6 allowed", "2001:db8::/32", "", "[2001:db8::1]:443", 200},
		{"IPv6 not allowed", "2001:db8::/32", "", "[2001:db9::1]:443", 403},
		{"IPv4-mapped client matches an IPv4 range", "10.0.0.0/8", "", "[::ffff:10.0.0.1]:443", 200},
		{"IPv4-mapped client denied by an IPv4 range", "", "10.0.0.0/8", "[::ffff:10.0.0.1]:443", 403},
		{"IPv4-mapped range matches an IPv4 client", "::ffff:10.0.0.0/104", "", "10.9.9.9:1", 200},
		{"IPv4 range doesn't match IPv6", "10.0.0.0/8", "", "[2001:db8::1]:443", 403},
		{"zoned link-local client", "fe80::/10", "", "[fe80::1%en0]:443", 200},
		{"address without a port", "10.0.0.0/8", "", "10.0.0.1", 200},
		{"RemoteAddr not an address", "10.0.0.0/8", "", "pipe", 403},
		{"RemoteAddr not an address, deny list only", "", "10.0.0.0/8", "@", 403},
		{"allow all IPv4 only", "0.0.0.0/0", "", "[2001:db8::1]:443", 403},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mw, err := ipfilter.New(prefixes(t, tt.allow), prefixes(t, tt.deny))
			if err != nil {
				t.Fatal(err)
			}
			called := false
			h := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
			req := httptest.NewRequest(http.MethodGet, "/ops/system", nil)
			req.RemoteAddr = tt.remoteAddr
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tt.want || called != (tt.want == 200) {
				t.Fatalf("status = %d, handler called %t; want %d", rec.Code, called, tt.want)
			}
			if tt.want == 403 && (problemCodeOf(t, rec.Body.Bytes()) != "ip_not_allowed" || strings.Contains(rec.Body.String(), tt.remoteAddr)) {
				t.Errorf("body = %s, want ip_not_allowed without the address", rec.Body.String())
			}
		})
	}
}

func TestBehindTrustedProxies(t *testing.T) {
	filter, err := ipfilter.New(prefixes(t, "198.51.100.0/24"), nil)
	if err != nil {
		t.Fatal(err)
	}
	h := httpx.Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		httpx.TrustedProxies(prefixes(t, "10.0.0.0/8")), filter)
	for _, tt := range []struct {
		peer, forwarded string
		want            int
	}{
		{"10.0.0.2:1", "198.51.100.7", 200},              // the proxy forwards an allowed client
		{"10.0.0.2:1", "203.0.113.1", 403},               // the proxy forwards another client
		{"203.0.113.1:1", "198.51.100.7", 403},           // a client claiming an allowed address
		{"10.0.0.2:1", "198.51.100.7, 203.0.113.1", 403}, // the right-most untrusted hop counts
	} {
		req := httptest.NewRequest(http.MethodGet, "/ops/system", nil)
		req.RemoteAddr = tt.peer
		req.Header.Set("X-Forwarded-For", tt.forwarded)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != tt.want {
			t.Errorf("peer %s forwarding %q: status = %d, want %d", tt.peer, tt.forwarded, rec.Code, tt.want)
		}
	}
}

func TestRefusesBadRanges(t *testing.T) {
	tests := []struct {
		name        string
		allow, deny []netip.Prefix
		wantErr     error
	}{
		{"deny every IPv4 address", nil, []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0")}, ipfilter.ErrDenyAll},
		{"deny every IPv6 address", nil, []netip.Prefix{netip.MustParsePrefix("::/0")}, ipfilter.ErrDenyAll},
		{"deny every IPv4 address, mapped", nil, []netip.Prefix{netip.MustParsePrefix("::ffff:0.0.0.0/96")}, ipfilter.ErrDenyAll},
		{"invalid range", []netip.Prefix{{}}, nil, nil},
		{"mapped range mixing families", []netip.Prefix{netip.MustParsePrefix("::ffff:0:0/80")}, nil, nil},
		{"allow inside deny", []netip.Prefix{netip.MustParsePrefix("10.0.5.0/24")}, []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}, nil},
		{"allow equal to deny", []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}, []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}, nil},
	}
	for _, tt := range tests {
		mw, err := ipfilter.New(tt.allow, tt.deny)
		if err == nil || mw != nil || tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
			t.Errorf("%s: IPFilter() = %v, %v; want an error", tt.name, mw != nil, err)
		}
	}
	// Both empty: the handler is returned unchanged.
	mw, err := ipfilter.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })).ServeHTTP(httptest.NewRecorder(), &http.Request{RemoteAddr: "junk"})
	if !called {
		t.Error("IPFilter(nil, nil) refused a request")
	}
}

func TestDoesntKeepCallerSlices(t *testing.T) {
	allow := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	mw, err := ipfilter.New(allow, nil)
	if err != nil {
		t.Fatal(err)
	}
	allow[0] = netip.MustParsePrefix("0.0.0.0/0")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.1:1"
	rec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d after the caller changed its slice", rec.Code)
	}
}

func TestParsePrefixes(t *testing.T) {
	got, err := ipfilter.ParsePrefixes(" 10.0.0.5/8, 192.0.2.10 ,2001:db8::1/32,::ffff:10.0.0.5,, 0.0.0.0/0")
	want := []string{"10.0.0.0/8", "192.0.2.10/32", "2001:db8::/32", "10.0.0.5/32", "0.0.0.0/0"}
	strs := make([]string, len(got))
	for i, p := range got {
		strs[i] = p.String()
	}
	if err != nil || !slices.Equal(strs, want) {
		t.Errorf("ParsePrefixes() = %v, %v; want %v", strs, err, want)
	}
	for _, bad := range []string{"load-balancer", "10.0.0.0/33", "fe80::1%en0", "::ffff:0:0/80", "10.0.0.0/8;", "10.0.0.0/8 192.0.2.1"} {
		if p, err := ipfilter.ParsePrefixes(bad); err == nil {
			t.Errorf("ParsePrefixes(%q) = %v, want an error", bad, p)
		}
	}
	if p, err := ipfilter.ParsePrefixes(" , "); p != nil || err != nil {
		t.Errorf("ParsePrefixes(empty) = %v, %v", p, err)
	}
}

func BenchmarkNew(b *testing.B) {
	mw, err := ipfilter.New(prefixes(b, "10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 2001:db8::/32"), prefixes(b, "10.0.5.0/24"))
	if err != nil {
		b.Fatal(err)
	}
	h := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "/ops/system", nil)
	req.RemoteAddr = "192.168.1.20:5555"
	w := httptest.NewRecorder()
	b.ReportAllocs()
	for b.Loop() {
		h.ServeHTTP(w, req)
	}
}
