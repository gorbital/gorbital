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
// It refuses ranges covering every IPv4 or IPv6 address ([ErrTrustAll]).
func ParseTrustedProxies(list string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, item := range strings.Split(list, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		p, err := netip.ParsePrefix(item)
		if err != nil {
			addr, addrErr := netip.ParseAddr(item)
			if addrErr != nil {
				return nil, fmt.Errorf("httpx: trusted proxy %q is not a CIDR range or IP address", item)
			}
			p = netip.PrefixFrom(addr, addr.BitLen())
		}
		if p.Bits() == 0 {
			return nil, fmt.Errorf("%w: %q", ErrTrustAll, item)
		}
		out = append(out, p.Masked())
	}
	return out, nil
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
func TrustedProxies(trusted []netip.Prefix) Middleware {
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
