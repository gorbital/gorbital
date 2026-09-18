package domain

import (
	"fmt"
	"net/netip"
	"net/url"
	"strings"
)

// MaxURLLength is the longest endpoint URL the module accepts. The
// migration's CHECK constraint matches it. Incoming webhook URLs are around
// a hundred characters; 2000 is the length no browser, proxy or log line has
// ever refused, and a bound is what keeps a registration from becoming a way
// to store arbitrary data.
const MaxURLLength = 2000

// docs:start endpoint-policy

// A Policy decides which delivery targets this deployment will accept and
// dial. It is the whole difference between production and development:
// main.go builds the module with
// notifications.AllowPrivateTargets(os.Getenv("APP_ENV") != "production"),
// so a developer can point an endpoint at a server on their own machine and
// a production deployment refuses every private, loopback, link-local,
// unspecified, multicast and carrier-grade-NAT target, full stop. There is
// no setting, no flag and no per-organisation exception: a restaurant that
// could register http://169.254.169.254/... would be asking this server to
// read its own cloud credentials and post them somewhere.
type Policy struct {
	// AllowPrivateTargets lets an endpoint point inside the network this
	// server runs in. It is for development and for the tests, which post to
	// an httptest server on 127.0.0.1.
	AllowPrivateTargets bool
}

// carrierGradeNAT is 100.64.0.0/10 (RFC 6598), which net/netip has no
// predicate for. It is shared address space: inside a provider's network it
// reaches other tenants' machines, so it belongs with the private ranges.
var carrierGradeNAT = netip.MustParsePrefix("100.64.0.0/10")

// AllowsAddress reports whether the module may talk to addr. The sender
// calls it from the dialler's Control function, on the address actually
// being dialled, which is the only place the answer can be trusted.
func (p Policy) AllowsAddress(addr netip.Addr) bool {
	if p.AllowPrivateTargets {
		return true
	}
	// Unmap first: ::ffff:127.0.0.1 is loopback written as an IPv6 address,
	// and IsLoopback says no unless the v4 address is unwrapped.
	addr = addr.Unmap()
	switch {
	case !addr.IsValid(),
		addr.IsLoopback(),
		addr.IsPrivate(),
		addr.IsLinkLocalUnicast(),
		addr.IsLinkLocalMulticast(),
		addr.IsInterfaceLocalMulticast(),
		addr.IsMulticast(),
		addr.IsUnspecified(),
		carrierGradeNAT.Contains(addr):
		return false
	}
	return true
}

// docs:end endpoint-policy

// A URL is one endpoint's delivery target. It exists so that the URL cannot
// be printed by accident: String returns the redacted form, so a stray %v in
// a log line, an error or a response body shows the host and nothing else,
// and [URL.Secret] is the single, named way to get the real thing.
type URL struct {
	// raw is the whole URL, the secret. Unexported so that no encoder, no
	// formatter and no reflection-based logger can reach it.
	raw    string
	scheme string
	host   string
}

// docs:start parse-endpoint-url

// ParseURL returns the delivery target raw describes, or an error wrapping
// [ErrInvalidURL]. The error never quotes raw: what a restaurant typed may
// already be a live credential, and a validation message is the easiest
// place in a system for one to end up in a log.
//
// p decides whether a private target is acceptable. Everything else is
// refused whatever the policy says:
//
//   - a scheme other than https (http is allowed only where private targets
//     are, which is development), because the URL travels in the request and
//     plaintext would hand it to anything on the path;
//   - user information, because https://user:password@host is a second
//     credential this app has no business carrying;
//   - a fragment, because it is never sent to the server and its only use
//     here would be to hide something from a reader;
//   - an empty host, and anything over [MaxURLLength].
func ParseURL(raw string, p Policy) (URL, error) {
	raw = strings.TrimSpace(raw)
	switch {
	case raw == "":
		return URL{}, fmt.Errorf("%w: a URL is required", ErrInvalidURL)
	case len(raw) > MaxURLLength:
		return URL{}, fmt.Errorf("%w: must be at most %d characters", ErrInvalidURL, MaxURLLength)
	}
	u, err := url.Parse(raw)
	if err != nil {
		// url.Parse's error quotes the URL it could not parse, so it is
		// reported, never wrapped.
		return URL{}, fmt.Errorf("%w: must be a URL", ErrInvalidURL)
	}
	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && p.AllowPrivateTargets:
	default:
		return URL{}, fmt.Errorf("%w: must be an https URL", ErrInvalidURL)
	}
	switch {
	case u.User != nil:
		return URL{}, fmt.Errorf("%w: must not carry a user name or password", ErrInvalidURL)
	case u.Fragment != "" || u.RawFragment != "" || strings.Contains(raw, "#"):
		return URL{}, fmt.Errorf("%w: must not carry a fragment", ErrInvalidURL)
	case u.Host == "" || u.Hostname() == "":
		return URL{}, fmt.Errorf("%w: must name a host", ErrInvalidURL)
	}
	if err := checkHost(u.Hostname(), p); err != nil {
		return URL{}, err
	}
	return URL{raw: u.String(), scheme: u.Scheme, host: u.Host}, nil
}

// docs:end parse-endpoint-url

// docs:start endpoint-url-address

// checkHost refuses a literal IP address in a range the policy forbids.
//
// This check is worth having — it turns the obvious attempt, an endpoint
// spelled https://127.0.0.1:9200/, into a 422 at registration time with a
// message a restaurateur can act on — but it is not the protection. A *host
// name* is deliberately not resolved here: an answer now says nothing about
// what the name will resolve to when the delivery job runs, and resolving it
// would only add a lookup an attacker controls the timing of. The guard that
// actually holds is [Policy.AllowsAddress] in the sender's dialler, which
// runs on the address being connected to, every time, after DNS.
func checkHost(host string, p Policy) error {
	addr, err := netip.ParseAddr(strings.Trim(host, "[]"))
	literal := err == nil
	if !literal {
		// A host name, not a literal address. Only the dialler can judge it.
		return nil
	}
	if !p.AllowsAddress(addr) {
		return fmt.Errorf("%w: must not point at a private, loopback or link-local address", ErrInvalidURL)
	}
	return nil
}

// docs:end endpoint-url-address

// RestoreURL returns the URL stored in raw, without applying a [Policy].
//
// Rows read back from the database go through here rather than [ParseURL] on
// purpose: a policy tightened after a row was written — a deployment moving
// from development to production, or a range added to the forbidden list —
// must not make existing rows unreadable, which would turn a list endpoint
// into a 500 and hide the very row the restaurant needs to delete. The
// policy still applies where it matters: the sender refuses to dial the
// address whatever the row says.
func RestoreURL(raw string) URL {
	u, err := url.Parse(raw)
	if err != nil {
		return URL{raw: raw}
	}
	return URL{raw: raw, scheme: u.Scheme, host: u.Host}
}

// RestoreHost returns a URL that knows only its host.
//
// It exists for the list query, which asks PostgreSQL for the host and never
// selects the url column at all: the secret is then not in this process's
// memory on the path that renders a response, rather than being fetched and
// carefully not printed. The result IsZero, because there is nothing here to
// deliver to.
func RestoreHost(host string) URL { return URL{host: host} }

// Host returns the host, with its port if the URL names one. It is the only
// part of an endpoint URL the API returns, the logs carry and the audit
// events record: enough for a restaurant to recognise which of its channels
// a row is, and useless to anyone who steals it.
func (u URL) Host() string { return u.host }

// Redacted returns the scheme, the host and an ellipsis, such as
// "https://hooks.example.com/...". It is safe to print anywhere.
func (u URL) Redacted() string {
	if u.IsZero() {
		return ""
	}
	if u.scheme == "" {
		return u.host + "/..."
	}
	return u.scheme + "://" + u.host + "/..."
}

// String returns [URL.Redacted]. The String method exists so that the
// redacted form is what every %v, %s, print and structured log attribute
// produces: the safe rendering has to be the default one, because the
// dangerous one only has to escape once.
func (u URL) String() string { return u.Redacted() }

// Secret returns the whole URL, including the path that makes it a
// credential. This is the only way to get it, and the module's sender is the
// only caller: nothing else — no handler, no response, no log line, no audit
// event — may call it.
func (u URL) Secret() string { return u.raw }

// IsZero reports whether the URL is unset.
func (u URL) IsZero() bool { return u.raw == "" }
