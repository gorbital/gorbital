package gorbital

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"gorbital.dev/config"
	"gorbital.dev/modules/auth"
)

// devWebAuthnOrigins are the passkey origins in development: browsers allow
// passkeys on localhost over http (not on 127.0.0.1).
var devWebAuthnOrigins = []string{"http://localhost:8080", "http://localhost:3000"}

// loadAuthConfig reads the sign-in variables. A half-filled provider is an
// error naming what's missing, so it can't silently stay off (ADR-0045).
func loadAuthConfig(get func(string) string, secret func(string) config.Secret, production bool) (AuthConfig, []error) {
	var errs []error
	c := AuthConfig{EncryptionKeys: secret("AUTH_ENCRYPTION_KEYS")}
	if !c.EncryptionKeys.IsZero() {
		if _, err := auth.ParseKeyring(c.EncryptionKeys.Reveal()); err != nil {
			errs = append(errs, fmt.Errorf("AUTH_ENCRYPTION_KEYS: %w", err))
		}
	}

	c.WebAuthnRPID = get("WEBAUTHN_RP_ID")
	c.WebAuthnOrigins = splitList(get("WEBAUTHN_ORIGINS"))
	c.WebAuthnAppleAppIDs = get("WEBAUTHN_APPLE_APP_IDS")
	c.WebAuthnAndroidApps = get("WEBAUTHN_ANDROID_APPS")
	if c.WebAuthnRPID == "" && len(c.WebAuthnOrigins) == 0 && !production {
		c.WebAuthnRPID, c.WebAuthnOrigins = "localhost", slices.Clone(devWebAuthnOrigins)
	}
	switch {
	case c.WebAuthnRPID == "" && (len(c.WebAuthnOrigins) > 0 || c.WebAuthnAppleAppIDs != "" || c.WebAuthnAndroidApps != ""):
		errs = append(errs, errors.New("WEBAUTHN_RP_ID is required with WEBAUTHN_ORIGINS, WEBAUTHN_APPLE_APP_IDS or WEBAUTHN_ANDROID_APPS: your site's domain, such as example.com (see AUTH_PROVIDERS.md)"))
	case c.WebAuthnRPID != "" && len(c.WebAuthnOrigins) == 0:
		errs = append(errs, errors.New("WEBAUTHN_ORIGINS is required with WEBAUTHN_RP_ID: the browser origins that use passkeys, such as https://app.example.com (see AUTH_PROVIDERS.md)"))
	case c.WebAuthnRPID != "" && production:
		for _, origin := range c.WebAuthnOrigins {
			if !strings.HasPrefix(origin, "https://") {
				errs = append(errs, fmt.Errorf("WEBAUTHN_ORIGINS: %q must use https in production", origin))
			}
		}
	}

	c.PublicURL = strings.TrimRight(get("APP_PUBLIC_URL"), "/")
	if c.PublicURL == "" && !production {
		c.PublicURL = devPublicURL
	}
	c.DefaultReturnTo = get("AUTH_DEFAULT_RETURN_TO")

	c.GoogleClientID, c.GoogleClientSecret = get("GOOGLE_CLIENT_ID"), secret("GOOGLE_CLIENT_SECRET")
	c.GoogleIOSClientID, c.GoogleAndroidClientID = get("GOOGLE_IOS_CLIENT_ID"), get("GOOGLE_ANDROID_CLIENT_ID")
	switch {
	case c.GoogleClientID == "" && (!c.GoogleClientSecret.IsZero() || c.GoogleIOSClientID != "" || c.GoogleAndroidClientID != ""):
		errs = append(errs, errors.New("GOOGLE_CLIENT_ID is required with GOOGLE_CLIENT_SECRET, GOOGLE_IOS_CLIENT_ID or GOOGLE_ANDROID_CLIENT_ID: the Web application client ID (see AUTH_PROVIDERS.md#google-sign-in)"))
	case c.GoogleClientID != "" && c.GoogleClientSecret.IsZero():
		errs = append(errs, errors.New("GOOGLE_CLIENT_SECRET is required with GOOGLE_CLIENT_ID: the web client's secret (see AUTH_PROVIDERS.md#google-sign-in)"))
	}

	c.AppleTeamID, c.AppleServicesID, c.AppleKeyID = get("APPLE_TEAM_ID"), get("APPLE_SERVICES_ID"), get("APPLE_KEY_ID")
	c.ApplePrivateKey, c.AppleBundleIDs = secret("APPLE_PRIVATE_KEY"), splitList(get("APPLE_BUNDLE_IDS"))
	if c.AppleTeamID != "" || c.AppleKeyID != "" || !c.ApplePrivateKey.IsZero() || c.AppleServicesID != "" || len(c.AppleBundleIDs) > 0 {
		var missing []string
		for _, v := range []struct {
			name  string
			empty bool
		}{
			{"APPLE_TEAM_ID", c.AppleTeamID == ""},
			{"APPLE_KEY_ID", c.AppleKeyID == ""},
			{"APPLE_PRIVATE_KEY_FILE", c.ApplePrivateKey.IsZero()},
			{"APPLE_SERVICES_ID or APPLE_BUNDLE_IDS", c.AppleServicesID == "" && len(c.AppleBundleIDs) == 0},
		} {
			if v.empty {
				missing = append(missing, v.name)
			}
		}
		if len(missing) > 0 {
			errs = append(errs, fmt.Errorf("sign-in with Apple also needs %s (see AUTH_PROVIDERS.md#apple-sign-in)", strings.Join(missing, ", ")))
		}
	}

	c.GitHubClientID, c.GitHubClientSecret = get("GITHUB_CLIENT_ID"), secret("GITHUB_CLIENT_SECRET")
	switch {
	case c.GitHubClientID == "" && !c.GitHubClientSecret.IsZero():
		errs = append(errs, errors.New("GITHUB_CLIENT_ID is required with GITHUB_CLIENT_SECRET: the OAuth app's client ID (see AUTH_PROVIDERS.md#github-sign-in)"))
	case c.GitHubClientID != "" && c.GitHubClientSecret.IsZero():
		errs = append(errs, errors.New("GITHUB_CLIENT_SECRET is required with GITHUB_CLIENT_ID: the OAuth app's client secret (see AUTH_PROVIDERS.md#github-sign-in)"))
	}

	if c.webSignIn() {
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
// an absolute URL on an origin a sign-in may return to, without a fragment,
// https in production. The API's /docs is no default in production, where
// docs are off by default.
func (c *Config) checkDefaultReturnTo() error {
	a := &c.Auth
	if a.DefaultReturnTo == "" {
		switch {
		case !a.webSignIn():
			return nil
		case c.Production():
			return errors.New("AUTH_DEFAULT_RETURN_TO is required with Google, Apple or GitHub sign-in in production: " +
				"the page of your frontend a sign-in returns to when it names no return_to, such as https://app.example.com/signed-in (see AUTH_PROVIDERS.md#after-signing-in)")
		case !c.DocsEnabled:
			return errors.New("AUTH_DEFAULT_RETURN_TO is required with Google, Apple or GitHub sign-in when APP_DOCS_ENABLED=false: " +
				"the default return address is otherwise the API docs (see AUTH_PROVIDERS.md#after-signing-in)")
		}
		a.DefaultReturnTo = a.PublicURL + "/docs"
		return nil
	}
	u, err := url.Parse(a.DefaultReturnTo)
	switch {
	case err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.Fragment != "" || len(a.DefaultReturnTo) > 2048:
		return fmt.Errorf("AUTH_DEFAULT_RETURN_TO %q must be an absolute http or https URL without user information or a fragment", a.DefaultReturnTo)
	case c.Production() && u.Scheme != "https":
		return fmt.Errorf("AUTH_DEFAULT_RETURN_TO %q must use https in production", a.DefaultReturnTo)
	case !slices.Contains(c.returnOrigins(), strings.ToLower(u.Scheme+"://"+u.Host)):
		return fmt.Errorf("AUTH_DEFAULT_RETURN_TO %q must be on APP_PUBLIC_URL or an origin in APP_CORS_ORIGINS", a.DefaultReturnTo)
	}
	return nil
}

// returnOrigins are where a web sign-in may send the browser back: the API
// itself and the frontends allowed by APP_CORS_ORIGINS.
func (c Config) returnOrigins() []string {
	origins := []string{strings.ToLower(c.Auth.PublicURL)}
	for _, o := range c.CORSOrigins {
		origins = append(origins, strings.ToLower(strings.TrimRight(o, "/")))
	}
	return origins
}

func splitList(s string) []string {
	var out []string
	for v := range strings.SplitSeq(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
