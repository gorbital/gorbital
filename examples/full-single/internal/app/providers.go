package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	opsdomain "example.com/acme-api/internal/modules/ops/domain"
)

// providersGuide explains every sign-in method's setup: what to provide,
// where to find it and where to paste it.
const providersGuide = "AUTH_PROVIDERS.md"

// signInMethods reports each sign-in method and, for the ones that are off,
// the environment variables that turn them on (ADR-0045). It never includes
// secret values.
func (c Config) signInMethods() []opsdomain.SignInMethod {
	web := c.WebAuthn.RPID != ""
	ios := web && len(c.WebAuthn.AppleAppIDs) > 0
	android := web && len(c.WebAuthn.AndroidApps) > 0
	methods := []opsdomain.SignInMethod{
		{Key: "email_password", Name: "Email and password", Enabled: true},
		{
			Key: "authenticator_app", Name: "Authenticator apps (2FA)", Enabled: !c.AuthEncryptionKeys.IsZero(),
			Missing: []string{"AUTH_ENCRYPTION_KEYS"}, Guide: providersGuide + "#authenticator-apps",
		},
		{
			Key: "passkeys", Name: "Passkeys in browsers", Enabled: web,
			Missing: []string{"WEBAUTHN_RP_ID", "WEBAUTHN_ORIGINS"}, Guide: providersGuide + "#passkeys",
		},
		{
			Key: "passkeys_ios", Name: "Passkeys in iOS apps", Enabled: ios,
			Missing: []string{"WEBAUTHN_APPLE_APP_IDS"}, Guide: providersGuide + "#passkeys-in-ios-apps",
		},
		{
			Key: "passkeys_android", Name: "Passkeys in Android apps", Enabled: android,
			Missing: []string{"WEBAUTHN_ANDROID_APPS"}, Guide: providersGuide + "#passkeys-in-android-apps",
		},
	}
	if web {
		methods[2].Detail = "RP ID " + c.WebAuthn.RPID + "; origins " + strings.Join(c.WebAuthn.Origins, ", ")
	}
	if ios {
		methods[3].Detail = strings.Join(c.WebAuthn.AppleAppIDs, ", ")
	}
	if android {
		packages := make([]string, len(c.WebAuthn.AndroidApps))
		for i, app := range c.WebAuthn.AndroidApps {
			packages[i] = app.Package
		}
		methods[4].Detail = strings.Join(packages, ", ")
	}
	for i := range methods {
		if methods[i].Enabled {
			methods[i].Missing, methods[i].Guide = nil, ""
		}
	}
	return methods
}

// WriteSignInMethods prints which sign-in methods are on and, for the others,
// what to set in .env and where AUTH_PROVIDERS.md explains it. The app prints
// it at start in development; `go run ./cmd/api auth-providers` prints it
// anywhere.
func WriteSignInMethods(w io.Writer, cfg Config) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "Sign-in methods")
	for _, m := range cfg.signInMethods() {
		if m.Enabled {
			fmt.Fprintf(tw, "  ✓ %s\t%s\n", m.Name, m.Detail)
		} else {
			fmt.Fprintf(tw, "  – %s\tset %s in .env\t%s\n", m.Name, strings.Join(m.Missing, ", "), m.Guide)
		}
	}
	_ = tw.Flush()
}

// reportSignInMethods prints the sign-in methods in development and logs
// which are configured in production.
func (a *App) reportSignInMethods(ctx context.Context) {
	if !a.cfg.Production() {
		WriteSignInMethods(os.Stdout, a.cfg)
		return
	}
	for _, m := range a.cfg.signInMethods() {
		a.logger.InfoContext(ctx, "sign-in method", "method", m.Key, "configured", m.Enabled)
	}
}
