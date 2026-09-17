package ipfilter_test

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strings"
	"testing"

	"gorbital.dev/httpx/ipfilter"
)

// FuzzNew parses allow and deny lists and filters a client address:
// nothing panics, accepted lists are canonical, and the decision matches a
// direct reading of the rules.
func FuzzNew(f *testing.F) {
	for _, seed := range [][3]string{
		{"10.0.0.0/8, 192.0.2.10", "10.0.5.0/24", "10.0.5.1:1"},
		{"", "203.0.113.0/24", "203.0.113.9:80"},
		{"::ffff:10.0.0.0/104", "", "10.9.9.9:1"},
		{"2001:db8::/32", "0.0.0.0/0", "[2001:db8::1]:443"},
		{"fe80::/10", "", "[fe80::1%en0]:443"},
		{"10.0.0.0/8", "10.0.0.0/8", "pipe"},
		{"0.0.0.0/0,::/0", "::ffff:0:0/80", "[::ffff:1.2.3.4]:5"},
	} {
		f.Add(seed[0], seed[1], seed[2])
	}
	f.Fuzz(func(t *testing.T, allowList, denyList, remote string) {
		allow, errA := ipfilter.ParsePrefixes(allowList)
		deny, errD := ipfilter.ParsePrefixes(denyList)
		if errA != nil || errD != nil {
			return
		}
		for _, p := range slices.Concat(allow, deny) {
			if !p.IsValid() || p != p.Masked() || p.Addr().Is4In6() {
				t.Fatalf("ParsePrefixes returned non-canonical %v", p)
			}
		}
		mw, err := ipfilter.New(allow, deny)
		if err != nil {
			return
		}
		called := false
		req := &http.Request{RemoteAddr: remote, Header: http.Header{}}
		rec := httptest.NewRecorder()
		mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })).ServeHTTP(rec, req)
		if len(allow) == 0 && len(deny) == 0 {
			if !called {
				t.Fatal("an empty filter refused a request")
			}
			return
		}
		addr, ok := parseRemote(remote)
		want := ok && !containsAddr(deny, addr) && (len(allow) == 0 || containsAddr(allow, addr))
		if called != want {
			t.Fatalf("allow %v deny %v remote %q: called = %t, want %t", allow, deny, remote, called, want)
		}
	})
}

func parseRemote(s string) (netip.Addr, bool) {
	if ap, err := netip.ParseAddrPort(s); err == nil {
		return ap.Addr().Unmap().WithZone(""), true
	}
	addr, err := netip.ParseAddr(s)
	return addr.Unmap().WithZone(""), err == nil
}

func containsAddr(ranges []netip.Prefix, addr netip.Addr) bool {
	return slices.ContainsFunc(ranges, func(p netip.Prefix) bool { return p.Contains(addr) })
}

// FuzzParsePrefixes checks the parser never panics and that its canonical
// output parses back to the same ranges.
func FuzzParsePrefixes(f *testing.F) {
	for _, seed := range []string{
		" 10.0.0.5/8, 192.0.2.10 ,2001:db8::1/32,", "", "0.0.0.0/0", "::ffff:10.0.0.5", "::ffff:10.0.0.0/104",
		"::ffff:0:0/80", "fe80::1%en0", "fe80::/10", "load-balancer", ",,", "192.0.2.1/33", "::/0",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, list string) {
		got, err := ipfilter.ParsePrefixes(list)
		if err != nil {
			if got != nil {
				t.Fatalf("ParsePrefixes(%q) = %v with error %v", list, got, err)
			}
			return
		}
		strs := make([]string, len(got))
		for i, p := range got {
			if !p.IsValid() || p != p.Masked() || p.Addr().Is4In6() || p.Addr().Zone() != "" {
				t.Fatalf("ParsePrefixes(%q)[%d] = %v: not canonical", list, i, p)
			}
			strs[i] = p.String()
		}
		again, err := ipfilter.ParsePrefixes(strings.Join(strs, ","))
		if err != nil || !slices.Equal(again, got) {
			t.Fatalf("ParsePrefixes(%q) = %v; reparsed = %v, %v", list, got, again, err)
		}
	})
}
