package httpx

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// ErrTrustAll reports a trusted-proxy range covering every address, which
// would let any client choose its own IP.
var ErrTrustAll = errors.New("httpx: a trusted proxy range can't cover every address")

// ParseTrustedProxies reads a comma-separated list of CIDR ranges or single
// addresses, such as "10.0.0.0/8, 192.0.2.10". An empty list trusts nothing.
// Ranges are masked ("10.0.0.5/8" is 10.0.0.0/8), and IPv4 addresses written
// in IPv6 form ("::ffff:10.0.0.5") become IPv4, as peer addresses are
// compared; an address with a zone ("fe80::1%en0") is refused, as it could
// never match one. It refuses ranges covering every IPv4 or IPv6 address
// ([ErrTrustAll]), including "::ffff:0.0.0.0/96", which is every IPv4
// address written in IPv6 form.
func ParseTrustedProxies(list string) ([]netip.Prefix, error) {
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
				return nil, fmt.Errorf("httpx: trusted proxy %q is not a CIDR range or IP address", item)
			}
			p = netip.PrefixFrom(addr, addr.BitLen())
		}
		// Canonicalise before the trust-all check: "::ffff:0.0.0.0/96" is
		// 0.0.0.0/0 once unmapped, and trusts every IPv4 client.
		p, err = canonicalPrefix(p)
		if err != nil {
			return nil, err
		}
		if p.Bits() == 0 {
			return nil, fmt.Errorf("%w: %q", ErrTrustAll, item)
		}
		out = append(out, p)
	}
	return out, nil
}

// canonicalPrefix masks p and turns an IPv4-mapped IPv6 range into its IPv4
// range, which is what the addresses compared here are: remoteAddr and
// forwardedClient unmap every address they read, so a mapped range could
// never match. A mapped range shorter than /96 mixes families and is
// refused.
//
// It is ipfilter.canonicalPrefix (httpx/ipfilter/ipfilter.go) with this
// package's error wording; the two are duplicated rather than shared
// because ipfilter imports httpx, and the rule is small enough to state
// twice. Change both together.
func canonicalPrefix(p netip.Prefix) (netip.Prefix, error) {
	if !p.IsValid() {
		return netip.Prefix{}, fmt.Errorf("httpx: invalid trusted proxy range %v", p)
	}
	if p.Addr().Is4In6() {
		if p.Bits() < 96 {
			return netip.Prefix{}, fmt.Errorf("httpx: trusted proxy range %v mixes IPv4-mapped and IPv6 addresses; write the IPv4 range", p)
		}
		p = netip.PrefixFrom(p.Addr().Unmap(), p.Bits()-96)
	}
	return p.Masked(), nil
}

// canonicalPrefixes canonicalises every range it can and drops the rest, so
// a range that would never match, or that netip would re-base onto one the
// caller never wrote, trusts nothing instead. [ParseTrustedProxies] reports
// these as errors; this is for callers that build prefixes themselves.
func canonicalPrefixes(in []netip.Prefix) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(in))
	for _, p := range in {
		if c, err := canonicalPrefix(p); err == nil {
			out = append(out, c)
		}
	}
	return out
}

// TrustedProxies sets each request's RemoteAddr to its client's address when
// it arrives through trusted proxies (ADR-0052). For a request whose peer is
// in trusted, it walks X-Forwarded-For from the right, skips trusted
// addresses, and uses the first untrusted one. Requests from other peers
// keep their address and their forwarding headers are ignored, so clients
// can't choose their IP. With no trusted ranges it changes nothing.
//
// Install it first, after recovery, so logs, audit events and rate limits
// see the client.
//
// It canonicalises the ranges as [ParseTrustedProxies] does, and skips one
// that can't be: build the list with ParseTrustedProxies, which reports
// those as errors, rather than passing prefixes straight in.
func TrustedProxies(trusted []netip.Prefix) Middleware {
	trusted = canonicalPrefixes(trusted)
	return func(next http.Handler) http.Handler {
		if len(trusted) == 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if client, ok := forwardedClient(r, trusted); ok {
				r = r.Clone(r.Context())
				r.RemoteAddr = net.JoinHostPort(client.String(), "0")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// forwardedClient returns the client address a trusted peer forwarded.
func forwardedClient(r *http.Request, trusted []netip.Prefix) (netip.Addr, bool) {
	peer, ok := remoteAddr(r.RemoteAddr)
	if !ok || !isTrusted(peer, trusted) {
		return netip.Addr{}, false
	}
	var hops []string
	for _, header := range r.Header.Values("X-Forwarded-For") {
		hops = append(hops, strings.Split(header, ",")...)
	}
	for i := len(hops) - 1; i >= 0; i-- {
		addr, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			return netip.Addr{}, false // a malformed hop: keep the peer
		}
		addr = addr.Unmap()
		if !isTrusted(addr, trusted) {
			return addr, true
		}
	}
	return netip.Addr{}, false
}

// fromTrusted reports whether r's client address is in trusted.
func fromTrusted(r *http.Request, trusted []netip.Prefix) bool {
	addr, ok := remoteAddr(r.RemoteAddr)
	return ok && isTrusted(addr, trusted)
}

func remoteAddr(s string) (netip.Addr, bool) {
	if ap, err := netip.ParseAddrPort(s); err == nil {
		return ap.Addr().Unmap(), true
	}
	addr, err := netip.ParseAddr(s)
	return addr.Unmap(), err == nil
}

func isTrusted(addr netip.Addr, trusted []netip.Prefix) bool {
	for _, p := range trusted {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}
