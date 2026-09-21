package authhttp

import (
	"errors"
	"fmt"
	"strings"

	"gorbital.dev/gorbital"
	"gorbital.dev/modules/auth/passkey"
	"gorbital.dev/modules/auth/social"
)

// checkConfig returns the configuration problems gorbital.LoadConfig leaves
// to sign-in, in the order and words of a v0.1 app's LoadConfig.
func checkConfig(cfg gorbital.Config) []error {
	var errs []error
	if cfg.Production() && cfg.Auth.EncryptionKeys.IsZero() {
		errs = append(errs, errors.New(`AUTH_ENCRYPTION_KEYS is required in production: it encrypts two-factor authentication secrets (generate one: echo "k1:$(openssl rand -base64 32)")`))
	}
	w, webAuthnErrs := loadWebAuthn(cfg)
	errs = append(errs, webAuthnErrs...)
	if w.RPID != "" && len(w.Origins) > 0 {
		// The display name doesn't affect the check.
		if _, err := w.service("app"); err != nil {
			errs = append(errs, fmt.Errorf("WEBAUTHN_RP_ID and WEBAUTHN_ORIGINS: %w", err))
		}
	}
	a := cfg.Auth
	appleSet := a.AppleTeamID != "" || a.AppleKeyID != "" || !a.ApplePrivateKey.IsZero() || a.AppleServicesID != "" || len(a.AppleBundleIDs) > 0
	appleComplete := a.AppleTeamID != "" && a.AppleKeyID != "" && !a.ApplePrivateKey.IsZero() && (a.AppleServicesID != "" || len(a.AppleBundleIDs) > 0)
	if appleSet && appleComplete { // gorbital.LoadConfig names what an incomplete one misses
		if _, err := social.ParseApplePrivateKey([]byte(a.ApplePrivateKey.Reveal())); err != nil {
			errs = append(errs, fmt.Errorf("APPLE_PRIVATE_KEY_FILE: %w", err))
		}
	}
	return errs
}

// webAuthnConfig is the passkey relying party from WEBAUTHN_* (ADR-0044).
// An empty RPID turns passkeys off.
type webAuthnConfig struct {
	RPID        string
	Origins     []string
	AppleAppIDs []string
	AndroidApps []passkey.AndroidApp
}

// loadWebAuthn parses WEBAUTHN_APPLE_APP_IDS and WEBAUTHN_ANDROID_APPS
// beside the relying party gorbital.LoadConfig read.
func loadWebAuthn(cfg gorbital.Config) (webAuthnConfig, []error) {
	c := webAuthnConfig{RPID: cfg.Auth.WebAuthnRPID, Origins: cfg.Auth.WebAuthnOrigins}
	var errs []error
	var err error
	if c.AppleAppIDs, err = passkey.ParseAppleAppIDs(cfg.Auth.WebAuthnAppleAppIDs); err != nil {
		errs = append(errs, fmt.Errorf("WEBAUTHN_APPLE_APP_IDS: %w", err))
	}
	if c.AndroidApps, err = passkey.ParseAndroidApps(cfg.Auth.WebAuthnAndroidApps); err != nil {
		errs = append(errs, fmt.Errorf("WEBAUTHN_ANDROID_APPS: %w", err))
	}
	return c, errs
}

// service returns the passkey service named after the app, or nil when
// passkeys are off.
func (c webAuthnConfig) service(name string) (*passkey.Service, error) {
	if c.RPID == "" {
		return nil, nil
	}
	return passkey.New(passkey.Config{
		RPID: c.RPID, RPDisplayName: name, Origins: c.Origins, AppleAppIDs: c.AppleAppIDs, AndroidApps: c.AndroidApps,
	})
}

// providerEndpoints point Google, Apple and GitHub at a fake provider in
// tests.
type providerEndpoints struct {
	Google social.Endpoints
	Apple  social.Endpoints
	GitHub social.Endpoints
}

// providers returns the configured sign-in providers; nil when off.
// gorbital.LoadConfig and CheckConfig have validated the configuration.
func providers(cfg gorbital.Config, ep providerEndpoints) (google, apple, gitHub *social.Provider) {
	a := cfg.Auth
	if a.GoogleClientID != "" {
		google, _ = social.NewGoogle(social.GoogleConfig{
			ClientID: a.GoogleClientID, ClientSecret: a.GoogleClientSecret.Reveal(),
			NativeClientIDs: splitList(a.GoogleIOSClientID + "," + a.GoogleAndroidClientID),
			Endpoints:       ep.Google,
		})
	}
	if a.AppleServicesID != "" || len(a.AppleBundleIDs) > 0 {
		if key, err := social.ParseApplePrivateKey([]byte(a.ApplePrivateKey.Reveal())); err == nil {
			apple, _ = social.NewApple(social.AppleConfig{
				TeamID: a.AppleTeamID, KeyID: a.AppleKeyID, PrivateKey: key, ServicesID: a.AppleServicesID, BundleIDs: a.AppleBundleIDs, Endpoints: ep.Apple,
			})
		}
	}
	if a.GitHubClientID != "" {
		gitHub, _ = social.NewGitHub(social.GitHubConfig{ClientID: a.GitHubClientID, ClientSecret: a.GitHubClientSecret.Reveal(), Endpoints: ep.GitHub})
	}
	return google, apple, gitHub
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
