package app

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"apistock.dev/config"
	"apistock.dev/modules/auth/social"
)

// devPublicURL is the API's address in development.
const devPublicURL = "http://localhost:8080"

// socialConfig is Google and Apple sign-in from GOOGLE_*, APPLE_* and
// APP_PUBLIC_URL (ADR-0046). What to set and where to find each value:
// AUTH_PROVIDERS.md.
type socialConfig struct {
	// PublicURL is the API's public base URL; providers return to it.
	PublicURL string
	Google    googleConfig
	Apple     appleConfig
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

func (g googleConfig) enabled() bool { return g.ClientID != "" }
func (a appleConfig) web() bool      { return a.ServicesID != "" }
func (a appleConfig) native() bool   { return len(a.BundleIDs) > 0 }

// loadSocialConfig reads Google and Apple sign-in. A half-filled provider is
// an error naming what's missing, so it can't silently stay off (ADR-0045).
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
		for name, empty := range map[string]bool{
			"APPLE_TEAM_ID": a.TeamID == "", "APPLE_KEY_ID": a.KeyID == "", "APPLE_PRIVATE_KEY_FILE": a.PrivateKey.IsZero(),
			"APPLE_SERVICES_ID or APPLE_BUNDLE_IDS": !a.web() && !a.native(),
		} {
			if empty {
				missing = append(missing, name)
			}
		}
		if len(missing) > 0 {
			errs = append(errs, fmt.Errorf("sign-in with Apple also needs %s (see AUTH_PROVIDERS.md#apple-sign-in)", strings.Join(sortedStrings(missing), ", ")))
		} else if _, err := social.ParseApplePrivateKey([]byte(a.PrivateKey.Reveal())); err != nil {
			errs = append(errs, fmt.Errorf("APPLE_PRIVATE_KEY_FILE: %w", err))
		}
	}

	if g.enabled() || a.web() {
		u, err := url.Parse(c.PublicURL)
		switch {
		case c.PublicURL == "":
			errs = append(errs, errors.New("APP_PUBLIC_URL is required with Google or Apple sign-in: the API's public URL, such as https://api.example.com"))
		case err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.Path != "" || u.RawQuery != "":
			errs = append(errs, fmt.Errorf("APP_PUBLIC_URL %q must be a scheme and host, such as https://api.example.com", c.PublicURL))
		case production && u.Scheme != "https":
			errs = append(errs, fmt.Errorf("APP_PUBLIC_URL %q must use https in production", c.PublicURL))
		}
	}
	return c, errs
}

// providers returns the configured sign-in providers; nil when off.
// LoadConfig has validated the configuration.
func (c socialConfig) providers(ep providerEndpoints) (google, apple *social.Provider) {
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
	return google, apple
}

// providerEndpoints point Google and Apple at a fake provider in tests.
type providerEndpoints struct {
	Google social.Endpoints
	Apple  social.Endpoints
}

// returnOrigins are where a web sign-in may send the browser back: the API
// itself and the frontends allowed by APP_CORS_ORIGINS.
func (c Config) returnOrigins() []string {
	return append([]string{c.Social.PublicURL}, c.CORSOrigins...)
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

func sortedStrings(s []string) []string {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
	return s
}
