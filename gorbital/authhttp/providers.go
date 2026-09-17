package authhttp

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"text/tabwriter"

	"gorbital.dev/gorbital"
)

// providersGuide explains every sign-in method's setup: what to provide,
// where to find it and where to paste it.
const providersGuide = "AUTH_PROVIDERS.md"

// signInMethod is a sign-in method and whether it is configured, as
// GET /ops/auth/providers reports it (ADR-0045).
type signInMethod struct {
	Key     string
	Name    string
	Enabled bool
	// Detail says how an enabled method is configured, without secrets.
	Detail string
	// Missing are the environment variables that turn a method on, and
	// Guide where AUTH_PROVIDERS.md explains them.
	Missing []string
	Guide   string
}

// signInMethods reports each sign-in method and, for the ones that are off,
// the environment variables that turn them on (ADR-0045). It never includes
// secret values.
func signInMethods(cfg gorbital.Config) []signInMethod {
	w, _ := loadWebAuthn(cfg)
	a := cfg.Auth
	web := w.RPID != ""
	method := func(key, name string, enabled bool, detail string, missing []string, section string) signInMethod {
		if enabled {
			return signInMethod{Key: key, Name: name, Enabled: true, Detail: detail}
		}
		return signInMethod{Key: key, Name: name, Missing: missing, Guide: providersGuide + "#" + section}
	}
	packages := make([]string, len(w.AndroidApps))
	for i, app := range w.AndroidApps {
		packages[i] = app.Package
	}
	callback := func(provider string) string {
		return "callback " + a.PublicURL + "/v1/auth/" + provider + "/callback"
	}
	google, gitHub := a.GoogleClientID != "", a.GitHubClientID != ""
	return []signInMethod{
		{Key: "email_password", Name: "Email and password", Enabled: true},
		method("authenticator_app", "Authenticator apps (2FA)", !a.EncryptionKeys.IsZero(), "",
			[]string{"AUTH_ENCRYPTION_KEYS"}, "authenticator-apps"),
		method("passkeys", "Passkeys in browsers", web, "RP ID "+w.RPID+"; origins "+strings.Join(w.Origins, ", "),
			[]string{"WEBAUTHN_RP_ID", "WEBAUTHN_ORIGINS"}, "passkeys"),
		method("passkeys_ios", "Passkeys in iOS apps", web && len(w.AppleAppIDs) > 0, strings.Join(w.AppleAppIDs, ", "),
			[]string{"WEBAUTHN_APPLE_APP_IDS"}, "passkeys-in-ios-apps"),
		method("passkeys_android", "Passkeys in Android apps", web && len(packages) > 0, strings.Join(packages, ", "),
			[]string{"WEBAUTHN_ANDROID_APPS"}, "passkeys-in-android-apps"),
		method("google", "Google sign-in", google, callback("google"),
			[]string{"GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET"}, "google-sign-in"),
		method("google_ios", "Google sign-in in iOS apps", google && a.GoogleIOSClientID != "", a.GoogleIOSClientID,
			[]string{"GOOGLE_IOS_CLIENT_ID"}, "4-ios-app-optional"),
		method("google_android", "Google sign-in in Android apps", google && a.GoogleAndroidClientID != "", a.GoogleAndroidClientID,
			[]string{"GOOGLE_ANDROID_CLIENT_ID"}, "5-android-app-optional"),
		method("apple", "Apple sign-in", a.AppleServicesID != "", callback("apple"),
			[]string{"APPLE_TEAM_ID", "APPLE_SERVICES_ID", "APPLE_KEY_ID", "APPLE_PRIVATE_KEY_FILE"}, "apple-sign-in"),
		method("apple_ios", "Apple sign-in in iOS apps", len(a.AppleBundleIDs) > 0, strings.Join(a.AppleBundleIDs, ", "),
			[]string{"APPLE_BUNDLE_IDS"}, "apple-sign-in"),
		method("github", "GitHub sign-in", gitHub, callback("github"),
			[]string{"GITHUB_CLIENT_ID", "GITHUB_CLIENT_SECRET"}, "github-sign-in"),
	}
}

// writeSignInMethods prints which sign-in methods are on and, for the
// others, what to set in .env and where AUTH_PROVIDERS.md explains it.
func writeSignInMethods(w io.Writer, cfg gorbital.Config) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "Sign-in methods")
	for _, m := range signInMethods(cfg) {
		if m.Enabled {
			fmt.Fprintf(tw, "  ✓ %s\t%s\n", m.Name, m.Detail)
		} else {
			fmt.Fprintf(tw, "  – %s\tset %s in .env\t%s\n", m.Name, strings.Join(m.Missing, ", "), m.Guide)
		}
	}
	_ = tw.Flush()
}

// reportSignInMethods logs which sign-in methods are configured when the
// app is built. A v0.1 app printed the table of writeSignInMethods in
// development when it started; the auth-providers command prints it, and
// the log lines stay out of test output.
func reportSignInMethods(ctx context.Context, cfg gorbital.Config, logger *slog.Logger) {
	for _, m := range signInMethods(cfg) {
		logger.InfoContext(ctx, "sign-in method", "method", m.Key, "configured", m.Enabled)
	}
}
