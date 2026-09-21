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

// SignInMethods reports each sign-in method a v0.1 app has, whether cfg
// configures it and, for the ones that are off, the environment variables
// that turn them on and the section of AUTH_PROVIDERS.md that explains them
// (ADR-0045). It never includes secret values. The operations API lists
// them in GET /ops/auth/providers (gorbital.Platform.SignInMethods), and the
// auth-providers command prints them.
//
// A method the app doesn't serve ([Methods]) is reported off with no
// variables to set and a Detail saying so: naming variables that would
// change nothing would tell an operator to set them (ADR-0089).
func (a *Authenticator) SignInMethods(cfg gorbital.Config) []gorbital.SignInMethod {
	return signInMethods(cfg, a.opts)
}

// signInMethods is SignInMethods, which needs of the Authenticator only
// its options.
func signInMethods(cfg gorbital.Config, o options) []gorbital.SignInMethod {
	reported := signInMethodReport(cfg, o)
	methods := make([]gorbital.SignInMethod, len(reported))
	for i, r := range reported {
		methods[i] = r.report
	}
	return methods
}

// A signInMethodReported is one row of SignInMethods with what
// reportSignInMethods needs beside it: the sign-in method it belongs to,
// and whether the environment configures it whether or not the app serves
// it.
type signInMethodReported struct {
	method     Method
	configured bool
	report     gorbital.SignInMethod
}

// signInMethodReport is SignInMethods with the sign-in method of each row.
func signInMethodReport(cfg gorbital.Config, o options) []signInMethodReported {
	w, _ := loadWebAuthn(cfg)
	a := cfg.Auth
	web := w.RPID != ""
	method := func(key, name string, m Method, configured bool, detail string, missing []string, section string) signInMethodReported {
		r := signInMethodReported{method: m, configured: configured}
		switch {
		case !o.has(m):
			r.report = gorbital.SignInMethod{Key: key, Name: name, Detail: notServed(m)}
		case configured:
			r.report = gorbital.SignInMethod{Key: key, Name: name, Enabled: true, Detail: detail}
		default:
			r.report = gorbital.SignInMethod{Key: key, Name: name, Missing: missing, Guide: providersGuide + "#" + section}
		}
		return r
	}
	packages := make([]string, len(w.AndroidApps))
	for i, app := range w.AndroidApps {
		packages[i] = app.Package
	}
	callback := func(provider string) string {
		return "callback " + a.PublicURL + "/v1/auth/" + provider + "/callback"
	}
	google, gitHub := a.GoogleClientID != "", a.GitHubClientID != ""
	return []signInMethodReported{
		{method: MethodPassword, configured: true, report: gorbital.SignInMethod{Key: "email_password", Name: "Email and password", Enabled: true}},
		method("authenticator_app", "Authenticator apps (2FA)", MethodTOTP, !a.EncryptionKeys.IsZero(), "",
			[]string{"AUTH_ENCRYPTION_KEYS"}, "authenticator-apps"),
		method("passkeys", "Passkeys in browsers", MethodPasskeys, web, "RP ID "+w.RPID+"; origins "+strings.Join(w.Origins, ", "),
			[]string{"WEBAUTHN_RP_ID", "WEBAUTHN_ORIGINS"}, "passkeys"),
		method("passkeys_ios", "Passkeys in iOS apps", MethodPasskeys, web && len(w.AppleAppIDs) > 0, strings.Join(w.AppleAppIDs, ", "),
			[]string{"WEBAUTHN_APPLE_APP_IDS"}, "passkeys-in-ios-apps"),
		method("passkeys_android", "Passkeys in Android apps", MethodPasskeys, web && len(packages) > 0, strings.Join(packages, ", "),
			[]string{"WEBAUTHN_ANDROID_APPS"}, "passkeys-in-android-apps"),
		method("google", "Google sign-in", MethodSocial, google, callback("google"),
			[]string{"GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET"}, "google-sign-in"),
		method("google_ios", "Google sign-in in iOS apps", MethodSocial, google && a.GoogleIOSClientID != "", a.GoogleIOSClientID,
			[]string{"GOOGLE_IOS_CLIENT_ID"}, "4-ios-app-optional"),
		method("google_android", "Google sign-in in Android apps", MethodSocial, google && a.GoogleAndroidClientID != "", a.GoogleAndroidClientID,
			[]string{"GOOGLE_ANDROID_CLIENT_ID"}, "5-android-app-optional"),
		method("apple", "Apple sign-in", MethodSocial, a.AppleServicesID != "", callback("apple"),
			[]string{"APPLE_TEAM_ID", "APPLE_SERVICES_ID", "APPLE_KEY_ID", "APPLE_PRIVATE_KEY_FILE"}, "apple-sign-in"),
		method("apple_ios", "Apple sign-in in iOS apps", MethodSocial, len(a.AppleBundleIDs) > 0, strings.Join(a.AppleBundleIDs, ", "),
			[]string{"APPLE_BUNDLE_IDS"}, "apple-sign-in"),
		method("github", "GitHub sign-in", MethodSocial, gitHub, callback("github"),
			[]string{"GITHUB_CLIENT_ID", "GITHUB_CLIENT_SECRET"}, "github-sign-in"),
	}
}

// notServed is the detail of a method the app doesn't serve, naming the
// option that would serve it.
func notServed(m Method) string {
	return "not enabled in this app (authhttp.Methods without " + string(m) + ")"
}

// writeSignInMethods prints which sign-in methods are on and, for the
// others, what to set in .env and where AUTH_PROVIDERS.md explains it, or
// that the app doesn't serve them.
func writeSignInMethods(w io.Writer, cfg gorbital.Config, o options) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "Sign-in methods")
	for _, m := range signInMethods(cfg, o) {
		switch {
		case m.Enabled:
			fmt.Fprintf(tw, "  ✓ %s\t%s\n", m.Name, m.Detail)
		case len(m.Missing) == 0:
			fmt.Fprintf(tw, "  – %s\t%s\n", m.Name, m.Detail)
		default:
			fmt.Fprintf(tw, "  – %s\tset %s in .env\t%s\n", m.Name, strings.Join(m.Missing, ", "), m.Guide)
		}
	}
	_ = tw.Flush()
}

// reportSignInMethods logs which sign-in methods are configured when the
// app is built. A v0.1 app printed the table of writeSignInMethods in
// development when it started; the auth-providers command prints it, and
// the log lines stay out of test output. A provider the environment
// configures for a method the app doesn't serve is a warning, not an
// error: the app starts, and the developer who set the variables is told
// nothing will use them (ADR-0089).
func reportSignInMethods(ctx context.Context, cfg gorbital.Config, o options, logger *slog.Logger) {
	for _, m := range signInMethodReport(cfg, o) {
		logger.InfoContext(ctx, "sign-in method", "method", m.report.Key, "configured", m.configured, "served", o.has(m.method))
		if m.configured && !o.has(m.method) {
			logger.WarnContext(ctx, "sign-in method configured but not served", "method", m.report.Key,
				"detail", "add "+string(m.method)+" to authhttp.Methods to serve it")
		}
	}
}
