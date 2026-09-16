package app

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"gorbital.dev/config"
	"gorbital.dev/modules/auth/social"
)

// devPublicURL is the API's address in development.
const devPublicURL = "http://localhost:8080"

// socialConfig is Google, Apple and GitHub sign-in from GOOGLE_*, APPLE_*,
// GITHUB_*, APP_PUBLIC_URL and AUTH_DEFAULT_RETURN_TO (ADR-0046, ADR-0059).
// What to set and where to find each value: AUTH_PROVIDERS.md.
type socialConfig struct {
	// PublicURL is the API's public base URL; providers return to it.
	PublicURL string
	// DefaultReturnTo is where a web sign-in started without return_to sends
	// the browser back (AUTH_DEFAULT_RETURN_TO). Required in production with
	// a web sign-in provider; in development it defaults to the API docs.
	DefaultReturnTo string
	Google          googleConfig
	Apple           appleConfig
	GitHub          gitHubConfig
}

type googleConfig struct {
	ClientID        string
	ClientSecret    config.Secret
	IOSClientID     string
	AndroidClientID string
}

type appleConfig struct {
	TeamID     string
	ServicesID string
	KeyID      string
	PrivateKey config.Secret // APPLE_PRIVATE_KEY, or the file APPLE_PRIVATE_KEY_FILE names
	BundleIDs  []string
}

// gitHubConfig is a GitHub OAuth app (ADR-0059).
type gitHubConfig struct {
	ClientID     string
	ClientSecret config.Secret
}

func (g googleConfig) enabled() bool { return g.ClientID != "" }
func (g gitHubConfig) enabled() bool { return g.ClientID != "" }
func (a appleConfig) web() bool      { return a.ServicesID != "" }
func (a appleConfig) native() bool   { return len(a.BundleIDs) > 0 }

// web reports whether a provider signs people in through the browser, which
// needs APP_PUBLIC_URL and a default return address.
func (c socialConfig) web() bool { return c.Google.enabled() || c.Apple.web() || c.GitHub.enabled() }

// loadSocialConfig reads Google, Apple and GitHub sign-in. A half-filled
// provider is an error naming what's missing, so it can't silently stay off
// (ADR-0045).
func loadSocialConfig(get func(string) string, secret func(string) config.Secret, production bool) (socialConfig, []error) {
	c := socialConfig{
		PublicURL: strings.TrimRight(get("APP_PUBLIC_URL"), "/"),
		Google: googleConfig{
			ClientID: get("GOOGLE_CLIENT_ID"), ClientSecret: secret("GOOGLE_CLIENT_SECRET"),
			IOSClientID: get("GOOGLE_IOS_CLIENT_ID"), AndroidClientID: get("GOOGLE_ANDROID_CLIENT_ID"),
		},
		Apple: appleConfig{
			TeamID: get("APPLE_TEAM_ID"), ServicesID: get("APPLE_SERVICES_ID"), KeyID: get("APPLE_KEY_ID"),
			PrivateKey: secret("APPLE_PRIVATE_KEY"), BundleIDs: splitList(get("APPLE_BUNDLE_IDS")),
		},
		GitHub:          gitHubConfig{ClientID: get("GITHUB_CLIENT_ID"), ClientSecret: secret("GITHUB_CLIENT_SECRET")},
		DefaultReturnTo: get("AUTH_DEFAULT_RETURN_TO"),
	}
	if c.PublicURL == "" && !production {
		c.PublicURL = devPublicURL
	}
	var errs []error

	g := c.Google
	switch {
	case !g.enabled() && (!g.ClientSecret.IsZero() || g.IOSClientID != "" || g.AndroidClientID != ""):
		errs = append(errs, errors.New("GOOGLE_CLIENT_ID is required with GOOGLE_CLIENT_SECRET, GOOGLE_IOS_CLIENT_ID or GOOGLE_ANDROID_CLIENT_ID: the Web application client ID (see AUTH_PROVIDERS.md#google-sign-in)"))
	case g.enabled() && g.ClientSecret.IsZero():
		errs = append(errs, errors.New("GOOGLE_CLIENT_SECRET is required with GOOGLE_CLIENT_ID: the web client's secret (see AUTH_PROVIDERS.md#google-sign-in)"))
	}

	a := c.Apple
	if a.TeamID != "" || a.KeyID != "" || !a.PrivateKey.IsZero() || a.web() || a.native() {
		var missing []string
		for _, v := range []struct {
			name  string
			empty bool
		}{
			{"APPLE_TEAM_ID", a.TeamID == ""},
			{"APPLE_KEY_ID", a.KeyID == ""},
			{"APPLE_PRIVATE_KEY_FILE", a.PrivateKey.IsZero()},
			{"APPLE_SERVICES_ID or APPLE_BUNDLE_IDS", !a.web() && !a.native()},
		} {
			if v.empty {
				missing = append(missing, v.name)
			}
		}
		if len(missing) > 0 {
			errs = append(errs, fmt.Errorf("sign-in with Apple also needs %s (see AUTH_PROVIDERS.md#apple-sign-in)", strings.Join(missing, ", ")))
		} else if _, err := social.ParseApplePrivateKey([]byte(a.PrivateKey.Reveal())); err != nil {
			errs = append(errs, fmt.Errorf("APPLE_PRIVATE_KEY_FILE: %w", err))
		}
	}

	switch gh := c.GitHub; {
	case !gh.enabled() && !gh.ClientSecret.IsZero():
		errs = append(errs, errors.New("GITHUB_CLIENT_ID is required with GITHUB_CLIENT_SECRET: the OAuth app's client ID (see AUTH_PROVIDERS.md#github-sign-in)"))
	case gh.enabled() && gh.ClientSecret.IsZero():
		errs = append(errs, errors.New("GITHUB_CLIENT_SECRET is required with GITHUB_CLIENT_ID: the OAuth app's client secret (see AUTH_PROVIDERS.md#github-sign-in)"))
	}

	if c.web() {
		u, err := url.Parse(c.PublicURL)
		switch {
		case c.PublicURL == "":
			errs = append(errs, errors.New("APP_PUBLIC_URL is required with Google, Apple or GitHub sign-in: the API's public URL, such as https://api.example.com"))
		case err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.Path != "" || u.RawQuery != "":
			errs = append(errs, fmt.Errorf("APP_PUBLIC_URL %q must be a scheme and host, such as https://api.example.com", c.PublicURL))
		case production && u.Scheme != "https":
			errs = append(errs, fmt.Errorf("APP_PUBLIC_URL %q must use https in production", c.PublicURL))
		}
	}
	return c, errs
}

// checkDefaultReturnTo validates AUTH_DEFAULT_RETURN_TO, or fills in the
// development default, once APP_CORS_ORIGINS and APP_DOCS_ENABLED are known:
// an absolute URL on an origin a sign-in may return to, without a fragment
// (the result goes there), https in production. The API's /docs is no
// default in production, where docs are off by default and a missing page
// would end every sign-in without return_to on a 404.
func (c *Config) checkDefaultReturnTo() error {
	s := &c.Social
	if s.DefaultReturnTo == "" {
		switch {
		case !s.web():
			return nil
		case c.Production():
			return errors.New("AUTH_DEFAULT_RETURN_TO is required with Google, Apple or GitHub sign-in in production: " +
				"the page of your frontend a sign-in returns to when it names no return_to, such as https://app.example.com/signed-in (see AUTH_PROVIDERS.md#after-signing-in)")
		case !c.DocsEnabled:
			return errors.New("AUTH_DEFAULT_RETURN_TO is required with Google, Apple or GitHub sign-in when APP_DOCS_ENABLED=false: " +
				"the default return address is otherwise the API docs (see AUTH_PROVIDERS.md#after-signing-in)")
		}
		s.DefaultReturnTo = s.PublicURL + "/docs"
		return nil
	}
	u, err := url.Parse(s.DefaultReturnTo)
	switch {
	case err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.Fragment != "" || len(s.DefaultReturnTo) > 2048:
		return fmt.Errorf("AUTH_DEFAULT_RETURN_TO %q must be an absolute http or https URL without user information or a fragment", s.DefaultReturnTo)
	case c.Production() && u.Scheme != "https":
		return fmt.Errorf("AUTH_DEFAULT_RETURN_TO %q must use https in production", s.DefaultReturnTo)
	case !slices.Contains(c.returnOrigins(), strings.ToLower(u.Scheme+"://"+u.Host)):
		return fmt.Errorf("AUTH_DEFAULT_RETURN_TO %q must be on APP_PUBLIC_URL or an origin in APP_CORS_ORIGINS", s.DefaultReturnTo)
	}
	return nil
}

// providers returns the configured sign-in providers; nil when off.
// LoadConfig has validated the configuration.
func (c socialConfig) providers(ep providerEndpoints) (google, apple, gitHub *social.Provider) {
	if g := c.Google; g.enabled() {
		google, _ = social.NewGoogle(social.GoogleConfig{
			ClientID: g.ClientID, ClientSecret: g.ClientSecret.Reveal(), NativeClientIDs: splitList(g.IOSClientID + "," + g.AndroidClientID),
			Endpoints: ep.Google,
		})
	}
	if a := c.Apple; a.web() || a.native() {
		if key, err := social.ParseApplePrivateKey([]byte(a.PrivateKey.Reveal())); err == nil {
			apple, _ = social.NewApple(social.AppleConfig{
				TeamID: a.TeamID, KeyID: a.KeyID, PrivateKey: key, ServicesID: a.ServicesID, BundleIDs: a.BundleIDs, Endpoints: ep.Apple,
			})
		}
	}
	if gh := c.GitHub; gh.enabled() {
		gitHub, _ = social.NewGitHub(social.GitHubConfig{ClientID: gh.ClientID, ClientSecret: gh.ClientSecret.Reveal(), Endpoints: ep.GitHub})
	}
	return google, apple, gitHub
}

// providerEndpoints point Google, Apple and GitHub at a fake provider in
// tests.
type providerEndpoints struct {
	Google social.Endpoints
	Apple  social.Endpoints
	GitHub social.Endpoints
}

// returnOrigins are where a web sign-in may send the browser back: the API
// itself and the frontends allowed by APP_CORS_ORIGINS.
func (c Config) returnOrigins() []string {
	origins := []string{strings.ToLower(c.Social.PublicURL)}
	for _, o := range c.CORSOrigins {
		origins = append(origins, strings.ToLower(strings.TrimRight(o, "/")))
	}
	return origins
}

func splitList(s string) []string {
	var out []string
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
