package tunnel

import (
	"net"
	"net/netip"
	"net/url"
	"slices"
	"strings"
)

// EnvChange is one proposed change to .env.
type EnvChange struct {
	Key string `json:"key"`
	// Current is the value .env (or the environment) has, "" when unset.
	Current string `json:"current"`
	// Proposed is the value the tunnel needs.
	Proposed string `json:"proposed"`
	Reason   string `json:"reason"`
	// Optional changes are proposed but not needed for sign-in to work
	// through the tunnel; the portal leaves them unticked.
	Optional bool `json:"optional"`
}

// Callback is an address to register with a provider for the tunnel's
// hostname.
type Callback struct {
	// Provider is google, apple, github or resend.
	Provider string `json:"provider"`
	// Label names the address, such as "Authorized redirect URI".
	Label string `json:"label"`
	// URL is the full address; Path the app's route it reaches.
	URL    string `json:"url"`
	Method string `json:"method"`
	Path   string `json:"path"`
	// Where is where to paste it in the provider's console.
	Where string `json:"where"`
	// Configured reports that the app's environment sets up the provider
	// (such as GOOGLE_CLIENT_ID).
	Configured bool `json:"configured"`
}

// Route is a route of the app, as the portal lists them.
type Route struct {
	Method string
	Path   string
}

// SetupInput is what Setup needs.
type SetupInput struct {
	// Status is the tunnel's; Setup needs its PublicURL.
	Status Status
	// Env is the environment as the app would read it (.env with the
	// process environment winning).
	Env []string
	// AppPort is the port the app listens on.
	AppPort string
	// Routes are the app's routes; RoutesKnown is false when they couldn't
	// be listed, and the provider addresses are then the defaults.
	Routes      []Route
	RoutesKnown bool
}

// Setup is the Tunnel screen's configuration help for a running tunnel.
type Setup struct {
	PublicURL string `json:"public_url"`
	Hostname  string `json:"hostname"`
	Stable    bool   `json:"stable"`
	// Changes are the .env changes for sign-in, passkeys and client
	// addresses through the tunnel; Set holds the required ones as the env
	// editor's PUT body takes them.
	Changes []EnvChange       `json:"changes"`
	Set     map[string]string `json:"set"`
	// Callbacks are the addresses to register with providers.
	Callbacks []Callback `json:"callbacks"`
	// RoutesKnown is false when the addresses are the defaults because the
	// app's routes couldn't be listed.
	RoutesKnown bool `json:"routes_known"`
	// Warnings say what to watch out for, such as a quick tunnel's URL
	// changing on the next run.
	Warnings []string `json:"warnings"`
}

// providerRoute is a route a provider calls back.
type providerRoute struct {
	provider, method, path, label, where string
	// configuredBy lists variables of which any being set means the app
	// uses the provider.
	configuredBy []string
}

// providerRoutes are the routes of gorbital's sign-in module and mail
// events module that providers call, with where each is registered
// (docs/guides/auth-providers.md). Setup lists those the app serves.
var providerRoutes = []providerRoute{
	{"google", "GET", "/v1/auth/google/callback", "Authorized redirect URI", "Google Cloud Console → Google Auth Platform → Clients → your Web application client → Authorized redirect URIs", []string{"GOOGLE_CLIENT_ID"}},
	{"apple", "POST", "/v1/auth/apple/callback", "Return URL", "Apple Developer → Certificates, Identifiers & Profiles → Identifiers → your Services ID → Sign in with Apple → Configure → Return URLs (and the hostname under Domains and Subdomains)", []string{"APPLE_SERVICES_ID"}},
	{"apple", "POST", "/v1/auth/apple/notifications", "Server-to-server notification endpoint", "Apple Developer → Identifiers → your App ID → Sign in with Apple → Configure → Server-to-Server Notification Endpoint", []string{"APPLE_SERVICES_ID", "APPLE_BUNDLE_IDS"}},
	{"github", "GET", "/v1/auth/github/callback", "Authorization callback URL", "GitHub → Settings → Developer settings → OAuth Apps → an OAuth App for this hostname → Authorization callback URL (one URL per app)", []string{"GITHUB_CLIENT_ID"}},
	{"resend", "POST", "/v1/webhooks/resend", "Webhook endpoint", "Resend → Webhooks → Add endpoint (events email.bounced and email.complained); copy its signing secret to RESEND_WEBHOOK_SECRET (without it the route answers 404)", []string{"RESEND_WEBHOOK_SECRET", "RESEND_API_KEY"}},
}

// loopbackProxies are what APP_TRUSTED_PROXIES needs so the app reads the
// visitor's address from cloudflared's X-Forwarded-For.
var loopbackProxies = []string{"127.0.0.1/32", "::1/128"}

// BuildSetup proposes the .env changes and lists the provider addresses for
// a tunnel's public URL. It returns false when the tunnel has no URL yet.
func BuildSetup(in SetupInput) (Setup, bool) {
	origin := in.Status.PublicURL
	if origin == "" {
		return Setup{}, false
	}
	host := strings.TrimPrefix(origin, "https://")
	s := Setup{PublicURL: origin, Hostname: host, Stable: in.Status.Stable, RoutesKnown: in.RoutesKnown, Changes: []EnvChange{}, Set: map[string]string{}, Callbacks: []Callback{}, Warnings: []string{}}
	get := func(key string) string { return envValue(in.Env, key) }
	propose := func(key, proposed, reason string, optional bool) {
		current := get(key)
		if current == proposed {
			return
		}
		s.Changes = append(s.Changes, EnvChange{Key: key, Current: current, Proposed: proposed, Reason: reason, Optional: optional})
		if !optional {
			s.Set[key] = proposed
		}
	}

	propose("APP_PUBLIC_URL", origin, "Google, Apple and GitHub send people back to APP_PUBLIC_URL/v1/auth/<provider>/callback, and emails link to it", false)
	propose("WEBAUTHN_RP_ID", host, "passkeys belong to a domain (the relying party): the tunnel's hostname, served over HTTPS", false)
	propose("WEBAUTHN_ORIGINS", origin, "the browser origin that registers and uses passkeys must match WEBAUTHN_RP_ID; localhost origins can't be listed with it", false)

	if rt := get("AUTH_DEFAULT_RETURN_TO"); rt != "" {
		if u, err := url.Parse(rt); err == nil && isLoopbackName(u.Hostname()) {
			proposed := origin + "/docs"
			reason := "it points at this machine, which a phone or a provider's redirect can't reach; the tunnel's API docs work instead (a local frontend isn't tunnelled)"
			if u.Port() == in.AppPort || (u.Port() == "" && in.AppPort == "80") {
				proposed = origin + u.EscapedPath()
				if u.RawQuery != "" {
					proposed += "?" + u.RawQuery
				}
				reason = "it points at the app on this machine; the same page through the tunnel works from anywhere"
			}
			propose("AUTH_DEFAULT_RETURN_TO", proposed, reason, false)
		}
	}

	if cors := get("APP_CORS_ORIGINS"); strings.TrimSpace(cors) != "" {
		origins := splitList(cors)
		if !slices.Contains(origins, origin) {
			propose("APP_CORS_ORIGINS", strings.Join(append(origins, origin), ","), "the app checks browser origins; list the tunnel's when a page served through it calls the API or is a sign-in return address", true)
		}
	}

	if proxies := get("APP_TRUSTED_PROXIES"); !trustsLoopback(proxies) {
		list := splitList(proxies)
		for _, p := range loopbackProxies {
			if !slices.Contains(list, p) {
				list = append(list, p)
			}
		}
		propose("APP_TRUSTED_PROXIES", strings.Join(list, ","), "cloudflared connects from this machine and passes the visitor's address in X-Forwarded-For: trusting loopback gives rate limits, logs and audit events the visitor's address (the dev console still refuses forwarded requests)", false)
	}

	served := map[string]bool{}
	for _, r := range in.Routes {
		served[strings.ToUpper(r.Method)+" "+r.Path] = true
	}
	for _, pr := range providerRoutes {
		if in.RoutesKnown && !served[pr.method+" "+pr.path] {
			continue
		}
		configured := false
		for _, key := range pr.configuredBy {
			if get(key) != "" || get(key+"_FILE") != "" {
				configured = true
			}
		}
		s.Callbacks = append(s.Callbacks, Callback{Provider: pr.provider, Label: pr.label, URL: origin + pr.path, Method: pr.method, Path: pr.path, Where: pr.where, Configured: configured})
	}

	if !in.Status.Stable {
		s.Warnings = append(s.Warnings, "A quick tunnel's URL changes every time it starts: callbacks registered with Google, Apple or GitHub and passkeys created on it stop working next run. Use it for webhooks and trying the API from a phone; use a named tunnel for sign-in and passkeys.")
	}
	if get("WEBAUTHN_APPLE_APP_IDS") != "" || get("WEBAUTHN_ANDROID_APPS") != "" {
		s.Warnings = append(s.Warnings, "Native apps' passkeys check /.well-known files on WEBAUTHN_RP_ID's domain: they work through the tunnel only when the apps' associated domains list this hostname.")
	}
	s.Warnings = append(s.Warnings, "While the tunnel runs, the app is on the internet: sign-in, public routes and anything a signed-in account can do. The dev console (/_dev) and the Dev Portal are not reachable through it.")
	return s, true
}

// isLoopbackName reports a host naming this machine.
func isLoopbackName(host string) bool {
	if strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsUnspecified())
}

// trustsLoopback reports whether a APP_TRUSTED_PROXIES value covers both
// 127.0.0.1 and ::1.
func trustsLoopback(list string) bool {
	v4, v6 := false, false
	for _, item := range splitList(list) {
		p, err := netip.ParsePrefix(item)
		if err != nil {
			a, aerr := netip.ParseAddr(item)
			if aerr != nil {
				continue
			}
			p = netip.PrefixFrom(a, a.BitLen())
		}
		v4 = v4 || p.Contains(netip.MustParseAddr("127.0.0.1"))
		v6 = v6 || p.Contains(netip.IPv6Loopback())
	}
	return v4 && v6
}

func splitList(s string) []string {
	var out []string
	for item := range strings.SplitSeq(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}
