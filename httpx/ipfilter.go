package httpx

import (
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strings"
)

// ErrDenyAll reports a deny range covering every IPv4 or IPv6 address,
// which would refuse every request of that family.
var ErrDenyAll = errors.New("httpx: a deny range can't cover every address; list the allowed ranges instead")

// ParsePrefixes reads a comma-separated list of CIDR ranges or single
// addresses, such as "10.0.0.0/8, 2001:db8::/32, 192.0.2.10", for
// [IPFilter]. Ranges are masked ("10.0.0.5/8" is 10.0.0.0/8), and IPv4
// addresses written in IPv6 form ("::ffff:10.0.0.5") become IPv4, as client
// addresses are compared. An empty list returns nil.
func ParsePrefixes(list string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for item := range strings.SplitSeq(list, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		p, err := netip.ParsePrefix(item)
		if err != nil {
			addr, addrErr := netip.ParseAddr(item)
			if addrErr != nil || addr.Zone() != "" {
				return nil, fmt.Errorf("httpx: %q is not a CIDR range or IP address", item)
			}
			p = netip.PrefixFrom(addr, addr.BitLen())
		}
		p, err = canonicalPrefix(p)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// canonicalPrefix masks p and turns an IPv4-mapped IPv6 range into its IPv4
// range. A mapped range shorter than /96 mixes families and is refused.
func canonicalPrefix(p netip.Prefix) (netip.Prefix, error) {
	if !p.IsValid() {
		return netip.Prefix{}, fmt.Errorf("httpx: invalid IP range %v", p)
	}
	if p.Addr().Is4In6() {
		if p.Bits() < 96 {
			return netip.Prefix{}, fmt.Errorf("httpx: IP range %v mixes IPv4-mapped and IPv6 addresses; write the IPv4 range", p)
		}
		p = netip.PrefixFrom(p.Addr().Unmap(), p.Bits()-96)
	}
	return p.Masked(), nil
}

// IPFilter returns middleware that refuses requests by client address with
// a 403 problem, code "ip_not_allowed". The address is the request's
// RemoteAddr, so behind a proxy install [TrustedProxies] first. A request
// is refused when its address is in a deny range, or when allow isn't empty
// and its address is in no allow range: deny wins, and an empty allow list
// allows every address not denied. A RemoteAddr that isn't an IP address is
// refused. IPv4-mapped IPv6 addresses are compared as IPv4, and IPv6 zones
// are ignored.
//
// It returns an error for an invalid range, a deny range covering every
// address of a family ([ErrDenyAll]; list allowed ranges instead), or an
// allow range entirely inside a deny range, which could never match. With
// both lists empty the middleware changes nothing.
func IPFilter(allow, deny []netip.Prefix) (Middleware, error) {
	allow, err := canonicalPrefixes(allow)
	if err != nil {
		return nil, err
	}
	deny, err = canonicalPrefixes(deny)
	if err != nil {
		return nil, err
	}
	for _, d := range deny {
		if d.Bits() == 0 {
			return nil, fmt.Errorf("%w: %v", ErrDenyAll, d)
		}
		for _, a := range allow {
			if d.Bits() <= a.Bits() && d.Contains(a.Addr()) {
				return nil, fmt.Errorf("httpx: allow range %v is inside deny range %v and would never match", a, d)
			}
		}
	}
	return func(next http.Handler) http.Handler {
		if len(allow) == 0 && len(deny) == 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			addr, ok := remoteAddr(r.RemoteAddr)
			addr = addr.WithZone("") // ranges have no zones
			if !ok || isTrusted(addr, deny) || len(allow) > 0 && !isTrusted(addr, allow) {
				WriteProblem(w, r, NewProblem(http.StatusForbidden, "ip_not_allowed", "requests from this network address are not allowed"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}, nil
}

func canonicalPrefixes(in []netip.Prefix) ([]netip.Prefix, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]netip.Prefix, len(in))
	for i, p := range in {
		c, err := canonicalPrefix(p)
		if err != nil {
			return nil, err
		}
		out[i] = c
	}
	return out, nil
}
