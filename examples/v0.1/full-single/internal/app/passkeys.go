package app

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"gorbital.dev/modules/auth/passkey"
)

// devWebAuthn is the passkey relying party in development: browsers allow
// passkeys on localhost over http (not on 127.0.0.1).
var devWebAuthn = webAuthnConfig{RPID: "localhost", Origins: []string{"http://localhost:8080", "http://localhost:3000"}}

// webAuthnConfig is the passkey relying party from WEBAUTHN_* (ADR-0044). An
// empty RPID turns passkeys off. What to set and where to find each value:
// AUTH_PROVIDERS.md.
type webAuthnConfig struct {
	RPID        string
	Origins     []string
	AppleAppIDs []string
	AndroidApps []passkey.AndroidApp
}

// loadWebAuthnConfig reads WEBAUTHN_*. In development, without RP ID and
// origins, passkeys work on localhost; in production they're off until
// configured. A partial configuration is an error, so it can't silently stay
// off (ADR-0045).
func loadWebAuthnConfig(get func(string) string, production bool) (webAuthnConfig, []error) {
	c := webAuthnConfig{RPID: get("WEBAUTHN_RP_ID")}
	for _, origin := range strings.Split(get("WEBAUTHN_ORIGINS"), ",") {
		if origin = strings.TrimSpace(origin); origin != "" {
			c.Origins = append(c.Origins, origin)
		}
	}
	var errs []error
	var err error
	if c.AppleAppIDs, err = passkey.ParseAppleAppIDs(get("WEBAUTHN_APPLE_APP_IDS")); err != nil {
		errs = append(errs, fmt.Errorf("WEBAUTHN_APPLE_APP_IDS: %w", err))
	}
	if c.AndroidApps, err = passkey.ParseAndroidApps(get("WEBAUTHN_ANDROID_APPS")); err != nil {
		errs = append(errs, fmt.Errorf("WEBAUTHN_ANDROID_APPS: %w", err))
	}
	if c.RPID == "" && len(c.Origins) == 0 && !production {
		c.RPID, c.Origins = devWebAuthn.RPID, devWebAuthn.Origins
	}

	switch {
	case c.RPID == "" && (len(c.Origins) > 0 || len(c.AppleAppIDs) > 0 || len(c.AndroidApps) > 0):
		errs = append(errs, errors.New("WEBAUTHN_RP_ID is required with WEBAUTHN_ORIGINS, WEBAUTHN_APPLE_APP_IDS or WEBAUTHN_ANDROID_APPS: your site's domain, such as example.com (see AUTH_PROVIDERS.md)"))
	case c.RPID != "" && len(c.Origins) == 0:
		errs = append(errs, errors.New("WEBAUTHN_ORIGINS is required with WEBAUTHN_RP_ID: the browser origins that use passkeys, such as https://app.example.com (see AUTH_PROVIDERS.md)"))
	case c.RPID != "":
		for _, origin := range c.Origins {
			if production && !strings.HasPrefix(origin, "https://") {
				errs = append(errs, fmt.Errorf("WEBAUTHN_ORIGINS: %q must use https in production", origin))
			}
		}
		if _, err := c.service(); err != nil {
			errs = append(errs, fmt.Errorf("WEBAUTHN_RP_ID and WEBAUTHN_ORIGINS: %w", err))
		}
	}
	return c, errs
}

// service returns the passkey service, or nil when passkeys are off.
func (c webAuthnConfig) service() (*passkey.Service, error) {
	if c.RPID == "" {
		return nil, nil
	}
	return passkey.New(passkey.Config{
		RPID: c.RPID, RPDisplayName: ServiceName, Origins: c.Origins, AppleAppIDs: c.AppleAppIDs, AndroidApps: c.AndroidApps,
	})
}

// passkeys returns the passkey service, or nil when passkeys are off.
// LoadConfig has already validated the configuration.
func (c webAuthnConfig) passkeys() *passkey.Service {
	s, _ := c.service()
	return s
}

// mountWellKnown serves the files iOS and Android apps need to use passkeys
// for this site, when their apps are configured (ADR-0044). They must be
// reachable on the RP ID's domain: when a web frontend serves that domain, it
// proxies these paths to the API.
func (a *App) mountWellKnown(mux *http.ServeMux) {
	if doc := passkey.AppleAppSiteAssociation(a.cfg.WebAuthn.AppleAppIDs); doc != nil {
		mux.HandleFunc("GET /.well-known/apple-app-site-association", jsonDocument(doc))
	}
	if doc := passkey.AssetLinks(a.cfg.WebAuthn.AndroidApps); doc != nil {
		mux.HandleFunc("GET /.well-known/assetlinks.json", jsonDocument(doc))
	}
}

func jsonDocument(doc []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = w.Write(doc)
	}
}
