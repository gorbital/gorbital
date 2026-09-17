package httpx_test

import (
	"slices"
	"strings"
	"testing"

	"gorbital.dev/httpx"
)

func FuzzParseTrustedProxies(f *testing.F) {
	for _, seed := range []string{
		" 10.0.0.0/8, 192.0.2.10 ,2001:db8::/32,", "", "0.0.0.0/0", "::/0", "10.0.0.0/8,0.0.0.0/0",
		"load-balancer", "fe80::1%en0", "::ffff:10.0.0.5", "10.0.0.5/8", ",,", "192.0.2.1/33",
		"::ffff:10.0.0.0/104", "::ffff:0.0.0.0/96", "::ffff:0:0/80", "fe80::/10",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, list string) {
		got, err := httpx.ParseTrustedProxies(list)
		if err != nil {
			if got != nil {
				t.Fatalf("ParseTrustedProxies(%q) = %v with error %v, want nil", list, got, err)
			}
			return
		}
		var items int
		for item := range strings.SplitSeq(list, ",") {
			if strings.TrimSpace(item) != "" {
				items++
			}
		}
		if len(got) != items {
			t.Fatalf("ParseTrustedProxies(%q) returned %d ranges for %d items", list, len(got), items)
		}
		strs := make([]string, len(got))
		for i, p := range got {
			// Never a range covering every address, always masked, and
			// always canonical: an IPv4-mapped or zoned range could never
			// match the unmapped, unzoned addresses TrustedProxies compares
			// (the invariant of ipfilter's FuzzParsePrefixes).
			if !p.IsValid() || p.Bits() <= 0 || p != p.Masked() || !p.Contains(p.Addr()) ||
				p.Addr().Is4In6() || p.Addr().Zone() != "" {
				t.Fatalf("ParseTrustedProxies(%q)[%d] = %v: invalid, trust-all, unmasked or not canonical", list, i, p)
			}
			strs[i] = p.String()
		}
		// The canonical form parses back to the same ranges.
		again, err := httpx.ParseTrustedProxies(strings.Join(strs, ","))
		if err != nil || !slices.Equal(again, got) {
			t.Fatalf("ParseTrustedProxies(%q) = %v; reparsed = %v, %v", list, got, again, err)
		}
	})
}
