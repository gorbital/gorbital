package signintest

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/json"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/go-jose/go-jose/v4"
	"golang.org/x/net/publicsuffix"

	"gorbital.dev/gorbital"
	authlib "gorbital.dev/modules/auth"
	"gorbital.dev/modules/auth/passkey"
	"gorbital.dev/modules/auth/social"
)

var (
	googleClientID = regexp.MustCompile(`^[0-9]+-[a-z0-9]+\.apps\.googleusercontent\.com$`)
	appleTenChars  = regexp.MustCompile(`^[A-Z0-9]{10}$`)
	reverseDNS     = regexp.MustCompile(`^[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+$`)
	gitHubOAuthID  = regexp.MustCompile(`^(Ov23li[A-Za-z0-9]{14}|[0-9a-f]{20})$`)
	gitHubAppID    = regexp.MustCompile(`^(Iv1\.[0-9a-f]{16}|Iv23li[A-Za-z0-9]{14})$`)
	gitHubSecret   = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// Overview returns every method with its offline checks: nothing here
// touches the network or the database.
func (t *Tester) Overview() Overview {
	return Overview{
		PublicURL: t.cfg.App.Auth.PublicURL,
		Methods: []Method{
			t.googleMethod(), t.appleMethod(), t.gitHubMethod(),
			t.passkeysMethod(), t.authenticatorAppMethod(), t.emailMethod(),
		},
	}
}

func ok(code, message string, variables ...string) Check {
	return Check{Code: code, Status: StatusOK, Message: message, Variables: variables}
}

func warn(code, message, fix, link string, variables ...string) Check {
	return Check{Code: code, Status: StatusWarn, Message: message, Fix: fix, Link: link, Variables: variables}
}

func fail(code, message, fix, link string, variables ...string) Check {
	return Check{Code: code, Status: StatusFail, Message: message, Fix: fix, Link: link, Variables: variables}
}

func skip(code, message, fix string, variables ...string) Check {
	return Check{Code: code, Status: StatusSkip, Message: message, Fix: fix, Link: LinkEnvironment, Variables: variables}
}

// live describes a redirect test from socialUnavailable.
func (t *Tester) redirectLive(provider string) Live {
	if u := t.socialUnavailable(provider); u != nil {
		return Live{Kind: "redirect", Reason: u.Reason, Link: u.Link}
	}
	return Live{Kind: "redirect", Available: true}
}

// publicURLChecks are what every web provider's callback depends on:
// APP_PUBLIC_URL's scheme and host, the port the app listens on, and a
// quick tunnel's changing address.
func (t *Tester) publicURLChecks(provider string, httpAllowed func(host string) bool) []Check {
	cfg := t.cfg.App
	callback := t.CallbackURL(provider)
	u, err := url.Parse(cfg.Auth.PublicURL)
	if err != nil || u.Host == "" {
		return []Check{fail("public_url", "APP_PUBLIC_URL "+cfg.Auth.PublicURL+" isn't a URL", "set APP_PUBLIC_URL to the API's address, such as http://localhost:8080", LinkEnvironment, "APP_PUBLIC_URL")}
	}
	checks := []Check{ok("callback_url", "the callback URL to register is "+callback, "APP_PUBLIC_URL")}
	host := u.Hostname()
	switch {
	case u.Scheme == "https":
		checks = append(checks, ok("public_url", "APP_PUBLIC_URL uses https", "APP_PUBLIC_URL"))
	case httpAllowed(host):
		checks = append(checks, ok("public_url", "APP_PUBLIC_URL uses http on "+host+", which "+providerLabel(provider)+" accepts in development", "APP_PUBLIC_URL"))
	default:
		checks = append(checks, fail("public_url", providerLabel(provider)+" doesn't accept an http callback on "+host,
			"use https: start a tunnel and apply its .env changes", LinkTunnel, "APP_PUBLIC_URL"))
	}
	if isLocalHost(host) {
		if _, appPort, err := net.SplitHostPort(cfg.Addr); err == nil {
			port := u.Port()
			if port == "" {
				port = map[string]string{"http": "80", "https": "443"}[u.Scheme]
			}
			if port != appPort {
				checks = append(checks, fail("callback_port", "APP_PUBLIC_URL names port "+port+" but the app listens on "+cfg.Addr+", so the provider sends the browser where nothing answers",
					"set APP_PUBLIC_URL to http://localhost:"+appPort+", or point it at the proxy that forwards to the app", LinkEnvironment, "APP_PUBLIC_URL", "APP_ADDR"))
			} else {
				checks = append(checks, ok("callback_port", "the callback reaches this app on port "+appPort, "APP_PUBLIC_URL", "APP_ADDR"))
			}
		}
	}
	if strings.HasSuffix(strings.ToLower(host), ".trycloudflare.com") {
		checks = append(checks, warn("quick_tunnel", "APP_PUBLIC_URL is a quick tunnel, whose address changes every time it starts",
			"register the callback for now, or use a named tunnel with a hostname that stays", LinkTunnel, "APP_PUBLIC_URL"))
	}
	return checks
}

func googleHTTPAllowed(host string) bool {
	host = strings.ToLower(host)
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func (t *Tester) googleMethod() Method {
	a := t.cfg.App.Auth
	m := Method{Key: MethodGoogle, Name: "Google", Configured: t.cfg.Google != nil, Live: t.redirectLive(social.Google)}
	if !m.Configured {
		m.Checks = []Check{skip("google_configured", "Google sign-in is off", "set GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET from a Web application OAuth client", "GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET")}
		return m
	}
	m.CallbackURL, m.IDToken = t.CallbackURL(social.Google), true
	if googleClientID.MatchString(a.GoogleClientID) {
		m.Checks = append(m.Checks, ok("google_client_id_format", "GOOGLE_CLIENT_ID looks like a Google OAuth client ID", "GOOGLE_CLIENT_ID"))
	} else {
		m.Checks = append(m.Checks, fail("google_client_id_format", "GOOGLE_CLIENT_ID doesn't look like a Google OAuth client ID (<number>-<id>.apps.googleusercontent.com)",
			"copy the Client ID of the Web application client from Google Cloud console → APIs & Services → Credentials", LinkEnvironment, "GOOGLE_CLIENT_ID"))
	}
	secret := a.GoogleClientSecret.Reveal()
	switch {
	case strings.HasPrefix(secret, "GOCSPX-") && len(secret) >= 30:
		m.Checks = append(m.Checks, ok("google_client_secret", "GOOGLE_CLIENT_SECRET looks like a Google client secret", "GOOGLE_CLIENT_SECRET"))
	case strings.TrimSpace(secret) != secret:
		m.Checks = append(m.Checks, fail("google_client_secret", "GOOGLE_CLIENT_SECRET has spaces around it", "remove the spaces or quotes in .env", LinkEnvironment, "GOOGLE_CLIENT_SECRET"))
	default:
		m.Checks = append(m.Checks, warn("google_client_secret", "GOOGLE_CLIENT_SECRET doesn't start with GOCSPX-, as current Google client secrets do",
			"check it's the Web application client's secret; the network check tells for sure", LinkEnvironment, "GOOGLE_CLIENT_SECRET"))
	}
	for _, v := range []struct{ name, value string }{{"GOOGLE_IOS_CLIENT_ID", a.GoogleIOSClientID}, {"GOOGLE_ANDROID_CLIENT_ID", a.GoogleAndroidClientID}} {
		switch {
		case v.value == "":
		case !googleClientID.MatchString(v.value):
			m.Checks = append(m.Checks, fail("google_native_client_id", v.name+" doesn't look like a Google OAuth client ID", "copy the client ID of the iOS or Android client", LinkEnvironment, v.name))
		case v.value == a.GoogleClientID:
			m.Checks = append(m.Checks, warn("google_native_client_id", v.name+" is the web client's ID", "native apps have their own clients; the web client ID is already accepted", LinkEnvironment, v.name))
		default:
			m.Checks = append(m.Checks, ok("google_native_client_id", v.name+" looks like a Google OAuth client ID", v.name))
		}
	}
	m.Checks = append(m.Checks, t.publicURLChecks(social.Google, googleHTTPAllowed)...)
	return m
}

func (t *Tester) appleMethod() Method {
	a := t.cfg.App.Auth
	m := Method{Key: MethodApple, Name: "Apple", Configured: t.cfg.Apple != nil, Live: t.redirectLive(social.Apple)}
	if !m.Configured {
		m.Checks = []Check{skip("apple_configured", "Apple sign-in is off",
			"set APPLE_TEAM_ID, APPLE_KEY_ID, APPLE_PRIVATE_KEY_FILE and APPLE_SERVICES_ID (web) or APPLE_BUNDLE_IDS (iOS)",
			"APPLE_TEAM_ID", "APPLE_KEY_ID", "APPLE_PRIVATE_KEY_FILE", "APPLE_SERVICES_ID", "APPLE_BUNDLE_IDS")}
		return m
	}
	m.IDToken = true
	for _, v := range []struct{ name, value, what string }{{"APPLE_TEAM_ID", a.AppleTeamID, "Team ID (Apple Developer → Membership)"}, {"APPLE_KEY_ID", a.AppleKeyID, "Key ID of the Sign in with Apple key"}} {
		code := strings.ToLower(v.name) // apple_team_id, apple_key_id
		if appleTenChars.MatchString(v.value) {
			m.Checks = append(m.Checks, ok(code, v.name+" has the form of an Apple "+v.what, v.name))
		} else {
			m.Checks = append(m.Checks, fail(code, v.name+" isn't 10 capital letters and digits, as an Apple "+v.what+" is", "copy the "+v.what, LinkEnvironment, v.name))
		}
	}
	m.Checks = append(m.Checks, t.appleKeyCheck())
	if a.AppleServicesID != "" {
		m.CallbackURL = t.CallbackURL(social.Apple)
		switch {
		case !reverseDNS.MatchString(a.AppleServicesID):
			m.Checks = append(m.Checks, fail("apple_services_id", "APPLE_SERVICES_ID isn't a reverse-DNS identifier such as com.example.web", "copy the Services ID's identifier", LinkEnvironment, "APPLE_SERVICES_ID"))
		case slices.Contains(a.AppleBundleIDs, a.AppleServicesID):
			m.Checks = append(m.Checks, fail("apple_services_id", "APPLE_SERVICES_ID is also in APPLE_BUNDLE_IDS",
				"the web client is a Services ID of its own, distinct from the App ID (bundle ID)", LinkEnvironment, "APPLE_SERVICES_ID", "APPLE_BUNDLE_IDS"))
		default:
			m.Checks = append(m.Checks, ok("apple_services_id", "APPLE_SERVICES_ID "+a.AppleServicesID+" is the web client", "APPLE_SERVICES_ID"))
		}
		u, err := url.Parse(a.PublicURL)
		if err == nil && u.Scheme == "https" && !isLocalHost(u.Hostname()) && net.ParseIP(u.Hostname()) == nil {
			m.Checks = append(m.Checks, ok("apple_public_url_https", "APP_PUBLIC_URL is https on a domain, as Apple requires", "APP_PUBLIC_URL"))
		} else {
			m.Checks = append(m.Checks, fail("apple_public_url_https", "Apple rejects return URLs that aren't https on a real domain, and APP_PUBLIC_URL is "+a.PublicURL,
				"start a named tunnel (a quick tunnel's address changes), apply its .env changes, and register its callback URL as the Services ID's Return URL", LinkTunnel, "APP_PUBLIC_URL"))
		}
		m.Checks = append(m.Checks, ok("callback_url", "the Return URL to register is "+m.CallbackURL+" (Apple posts to it)", "APP_PUBLIC_URL"))
	} else {
		m.Checks = append(m.Checks, skip("apple_services_id", "Apple's web sign-in is off: only iOS apps (APPLE_BUNDLE_IDS) sign in", "set APPLE_SERVICES_ID for sign-in in browsers", "APPLE_SERVICES_ID"))
	}
	for _, id := range a.AppleBundleIDs {
		if !reverseDNS.MatchString(id) {
			m.Checks = append(m.Checks, fail("apple_bundle_ids", "APPLE_BUNDLE_IDS has "+id+", which isn't a bundle ID such as com.example.app", "list the iOS apps' bundle IDs, comma-separated", LinkEnvironment, "APPLE_BUNDLE_IDS"))
		}
	}
	return m
}

// appleKeyCheck loads the .p8 key and signs a client secret with it, as
// sign-in does for every Apple request, then verifies the signature.
func (t *Tester) appleKeyCheck() Check {
	a := t.cfg.App.Auth
	key, err := social.ParseApplePrivateKey([]byte(a.ApplePrivateKey.Reveal()))
	if err != nil {
		return fail("apple_private_key", "the Apple key doesn't load: "+redact(err.Error()), "point APPLE_PRIVATE_KEY_FILE at the .p8 file downloaded from Apple Developer → Keys", LinkEnvironment, "APPLE_PRIVATE_KEY_FILE")
	}
	if err := signsClientSecret(key, a.AppleTeamID, a.AppleKeyID, a.AppleServicesID, t.cfg.Now()); err != nil {
		return fail("apple_private_key", "the Apple key loads but can't sign a client secret: "+redact(err.Error()), "download the key again from Apple Developer → Keys", LinkEnvironment, "APPLE_PRIVATE_KEY_FILE")
	}
	return ok("apple_private_key", "the .p8 key loads and signs a client secret (ES256 JWT for team "+a.AppleTeamID+", key "+a.AppleKeyID+"); only Apple can tell whether they belong together: run the network check",
		"APPLE_PRIVATE_KEY_FILE")
}

// signsClientSecret signs a client secret like Apple's and verifies it
// with the key's public half.
func signsClientSecret(key *ecdsa.PrivateKey, teamID, keyID, clientID string, now time.Time) error {
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: jose.JSONWebKey{Key: key, KeyID: keyID}}, (&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"iss": teamID, "iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(), "aud": "https://appleid.apple.com", "sub": clientID})
	obj, err := signer.Sign(payload)
	if err != nil {
		return err
	}
	compact, err := obj.CompactSerialize()
	if err != nil {
		return err
	}
	parsed, err := jose.ParseSigned(compact, []jose.SignatureAlgorithm{jose.ES256})
	if err != nil {
		return err
	}
	got, err := parsed.Verify(&key.PublicKey)
	if err != nil {
		return err
	}
	if !bytes.Equal(got, payload) {
		return jose.ErrCryptoFailure
	}
	return nil
}

func (t *Tester) gitHubMethod() Method {
	a := t.cfg.App.Auth
	m := Method{Key: MethodGitHub, Name: "GitHub", Configured: t.cfg.GitHub != nil, Live: t.redirectLive(social.GitHub)}
	if !m.Configured {
		m.Checks = []Check{skip("github_configured", "GitHub sign-in is off", "set GITHUB_CLIENT_ID and GITHUB_CLIENT_SECRET from a GitHub OAuth app", "GITHUB_CLIENT_ID", "GITHUB_CLIENT_SECRET")}
		return m
	}
	m.CallbackURL = t.CallbackURL(social.GitHub)
	switch {
	case gitHubOAuthID.MatchString(a.GitHubClientID):
		m.Checks = append(m.Checks, ok("github_client_id_format", "GITHUB_CLIENT_ID looks like an OAuth app's client ID", "GITHUB_CLIENT_ID"))
	case gitHubAppID.MatchString(a.GitHubClientID):
		m.Checks = append(m.Checks, warn("github_client_id_format", "GITHUB_CLIENT_ID is a GitHub App's client ID, not an OAuth app's",
			"gorbital asks for the read:user and user:email scopes of an OAuth app; a GitHub App ignores scopes and needs the Email addresses (read) permission instead", LinkGuide, "GITHUB_CLIENT_ID"))
	default:
		m.Checks = append(m.Checks, fail("github_client_id_format", "GITHUB_CLIENT_ID doesn't look like a GitHub client ID (Ov23li… or 20 hex characters)",
			"copy the Client ID from GitHub → Settings → Developer settings → OAuth Apps", LinkEnvironment, "GITHUB_CLIENT_ID"))
	}
	if gitHubSecret.MatchString(a.GitHubClientSecret.Reveal()) {
		m.Checks = append(m.Checks, ok("github_client_secret_format", "GITHUB_CLIENT_SECRET looks like a GitHub client secret", "GITHUB_CLIENT_SECRET"))
	} else {
		m.Checks = append(m.Checks, warn("github_client_secret_format", "GITHUB_CLIENT_SECRET isn't 40 hex characters, as GitHub's client secrets are",
			"generate a client secret in the OAuth app and copy it whole; the network check tells for sure", LinkEnvironment, "GITHUB_CLIENT_SECRET"))
	}
	m.Checks = append(m.Checks, t.publicURLChecks(social.GitHub, func(string) bool { return true })...)
	return m
}

// passkeyOrigin is where the live ceremony runs: APP_PUBLIC_URL's origin,
// the one address the app serves that browsers use.
func (t *Tester) passkeyOrigin() string {
	u, err := url.Parse(t.cfg.App.Auth.PublicURL)
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.ToLower(u.Scheme + "://" + u.Host)
}

// passkeysUnavailable returns why the ceremony can't run, or nil.
func (t *Tester) passkeysUnavailable() *UnavailableError {
	a := t.cfg.App.Auth
	switch origin := t.passkeyOrigin(); {
	case t.cfg.Passkeys == nil:
		return &UnavailableError{Reason: "passkeys are off (WEBAUTHN_RP_ID)", Link: LinkEnvironment}
	case !slices.Contains(lower(a.WebAuthnOrigins), origin):
		return &UnavailableError{
			Reason: "the ceremony runs on the app's own origin " + origin + " (APP_PUBLIC_URL), which isn't in WEBAUTHN_ORIGINS (" + strings.Join(a.WebAuthnOrigins, ", ") +
				"). Passkeys created on those origins can still work; add " + origin + " to WEBAUTHN_ORIGINS to test here, or use a tunnel whose origin is listed",
			Link: LinkEnvironment,
		}
	}
	return nil
}

func lower(list []string) []string {
	out := make([]string, len(list))
	for i, v := range list {
		out[i] = strings.ToLower(strings.TrimRight(v, "/"))
	}
	return out
}

func (t *Tester) passkeysMethod() Method {
	a := t.cfg.App.Auth
	m := Method{Key: MethodPasskeys, Name: "Passkeys", Configured: t.cfg.Passkeys != nil, Live: Live{Kind: "ceremony", Available: true}, RPID: a.WebAuthnRPID, Origin: t.passkeyOrigin()}
	if u := t.passkeysUnavailable(); u != nil {
		m.Live = Live{Kind: "ceremony", Reason: u.Reason, Link: u.Link}
	}
	if !m.Configured {
		m.Checks = []Check{skip("webauthn_rp_id", "passkeys are off", "set WEBAUTHN_RP_ID (your site's domain) and WEBAUTHN_ORIGINS", "WEBAUTHN_RP_ID", "WEBAUTHN_ORIGINS")}
		return m
	}
	rp := strings.ToLower(a.WebAuthnRPID)
	switch {
	case net.ParseIP(rp) != nil:
		m.Checks = append(m.Checks, fail("webauthn_rp_id", "WEBAUTHN_RP_ID is an IP address; browsers accept only a domain", "use localhost in development or your domain, and open the app by that name", LinkEnvironment, "WEBAUTHN_RP_ID"))
	case rp == "localhost":
		m.Checks = append(m.Checks, ok("webauthn_rp_id", "WEBAUTHN_RP_ID is localhost: browsers allow passkeys on http://localhost (not 127.0.0.1)", "WEBAUTHN_RP_ID"))
	default:
		if suffix, _ := publicsuffix.PublicSuffix(rp); suffix == rp {
			m.Checks = append(m.Checks, fail("webauthn_rp_id", "WEBAUTHN_RP_ID "+rp+" is a public suffix, which no site can use", "use your registrable domain, such as example.com", LinkEnvironment, "WEBAUTHN_RP_ID"))
		} else {
			m.Checks = append(m.Checks, ok("webauthn_rp_id", "WEBAUTHN_RP_ID "+rp+" is a registrable domain", "WEBAUTHN_RP_ID"))
		}
		if strings.HasSuffix(rp, ".trycloudflare.com") {
			m.Checks = append(m.Checks, warn("quick_tunnel", "WEBAUTHN_RP_ID is a quick tunnel's name: passkeys made for it stop working when its address changes",
				"use a named tunnel on your own domain", LinkTunnel, "WEBAUTHN_RP_ID"))
		}
	}
	for _, origin := range a.WebAuthnOrigins {
		if err := passkey.CheckOrigin(a.WebAuthnRPID, origin); err != nil {
			m.Checks = append(m.Checks, fail("webauthn_origins", strings.TrimPrefix(err.Error(), passkey.ErrInvalidConfig.Error()+": "),
				"each origin must be https (http only on localhost) and on "+a.WebAuthnRPID+" or a subdomain of it", LinkEnvironment, "WEBAUTHN_ORIGINS"))
		} else {
			m.Checks = append(m.Checks, ok("webauthn_origins", origin+" can use passkeys for "+a.WebAuthnRPID, "WEBAUTHN_ORIGINS"))
		}
	}
	if m.Live.Available {
		m.Checks = append(m.Checks, ok("webauthn_live_origin", "the live ceremony runs on "+m.Origin+", an allowed origin", "APP_PUBLIC_URL", "WEBAUTHN_ORIGINS"))
	} else {
		m.Checks = append(m.Checks, warn("webauthn_live_origin", m.Live.Reason, "", LinkEnvironment, "APP_PUBLIC_URL", "WEBAUTHN_ORIGINS"))
	}
	return m
}

func (t *Tester) authenticatorAppMethod() Method {
	m := Method{Key: MethodAuthenticatorApp, Name: "Authenticator apps", Configured: t.cfg.Keyring != nil, Live: Live{Kind: "code", Available: true}}
	m.Checks = []Check{t.keyringCheck()}
	return m
}

// keyringCheck encrypts and decrypts a throwaway secret with
// AUTH_ENCRYPTION_KEYS, as enrolling an authenticator app does.
func (t *Tester) keyringCheck() Check {
	k := t.cfg.Keyring
	if k == nil {
		if t.cfg.App.Production() {
			return fail("auth_encryption_keys", "AUTH_ENCRYPTION_KEYS isn't set", "generate one: echo \"k1:$(openssl rand -base64 32)\"", LinkEnvironment, "AUTH_ENCRYPTION_KEYS")
		}
		return warn("auth_encryption_keys", "AUTH_ENCRYPTION_KEYS isn't set: accounts can't turn on authenticator apps, and production refuses to start without it",
			"generate one: echo \"k1:$(openssl rand -base64 32)\"", LinkEnvironment, "AUTH_ENCRYPTION_KEYS")
	}
	secret := authlib.NewTOTPSecret()
	aad := []byte("signintest")
	id, ciphertext, err := k.Encrypt([]byte(secret), aad)
	if err == nil {
		var plain []byte
		if plain, err = k.Decrypt(id, ciphertext, aad); err == nil && string(plain) != secret {
			err = jose.ErrCryptoFailure
		}
	}
	if err != nil {
		return fail("auth_encryption_keys", "AUTH_ENCRYPTION_KEYS can't encrypt and decrypt a secret: "+redact(err.Error()), "check each key is id:base64 of 32 bytes", LinkEnvironment, "AUTH_ENCRYPTION_KEYS")
	}
	return ok("auth_encryption_keys", "AUTH_ENCRYPTION_KEYS encrypts and decrypts authenticator app secrets with key "+k.CurrentKeyID(), "AUTH_ENCRYPTION_KEYS")
}

func (t *Tester) emailMethod() Method {
	cfg := t.cfg.App
	m := Method{Key: MethodEmail, Name: "Email", Configured: true, Live: Live{Kind: "email", Available: true}}
	switch cfg.MailDelivery {
	case gorbital.MailDevMail:
		m.Checks = []Check{ok("mail_delivery", "MAIL_DELIVERY is devmail: every message, verification codes included, lands in orb dev's inbox and none reaches a real address", "MAIL_DELIVERY")}
	case gorbital.MailMailpit:
		m.Checks = []Check{ok("mail_delivery", "MAIL_DELIVERY is mailpit: every message lands in Mailpit at "+cfg.MailpitAddr, "MAIL_DELIVERY", "MAILPIT_SMTP_ADDR")}
	default:
		c := ok("mail_delivery", "MAIL_DELIVERY is provider: messages go through the app's email provider to real addresses", "MAIL_DELIVERY")
		if cfg.Mail.ResendAPIKey.IsZero() {
			c = warn("mail_delivery", "MAIL_DELIVERY is provider and RESEND_API_KEY isn't set: the provider is whatever the app passes to gorbital.WithMailer",
				"send a test message to see whether the provider accepts it", LinkMail, "MAIL_DELIVERY", "RESEND_API_KEY")
		}
		m.Checks = []Check{c}
	}
	return m
}
