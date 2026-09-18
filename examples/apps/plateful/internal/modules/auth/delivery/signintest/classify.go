package signintest

import (
	"context"
	"errors"
	"net"
	"regexp"
	"strings"

	"gorbital.dev/modules/auth/social"
)

// failure is a classified error: a stable code, what happened and the fix.
type failure struct {
	code, message, fix, link string
}

// oauthErrorCode reads the error code golang.org/x/oauth2 puts in quotes
// first in a token endpoint's refusal: oauth2: "invalid_grant" "…".
var oauthErrorCode = regexp.MustCompile(`oauth2: "([A-Za-z0-9_.-]{1,64})"`)

// classify turns a provider error from social.Provider into a failure, with
// secrets and anything token-like removed from the message.
func (t *Tester) classify(provider string, err error, secrets ...string) failure {
	callback := t.CallbackURL(provider)
	msg := err.Error()
	detail := redact(strings.TrimPrefix(strings.TrimPrefix(msg, social.ErrExchange.Error()+": "), social.ErrInvalidToken.Error()+": "), append(secrets, t.clientSecrets()...)...)
	if m := oauthErrorCode.FindStringSubmatch(msg); m != nil {
		return describeFailure(provider, sanitizeCode(m[1]), detail, callback)
	}
	var netErr net.Error
	var dnsErr *net.DNSError
	lower := strings.ToLower(msg)
	switch {
	case errors.As(err, &netErr), errors.As(err, &dnsErr), errors.Is(err, context.DeadlineExceeded),
		strings.Contains(lower, "no such host"), strings.Contains(lower, "connection refused"), strings.Contains(lower, "i/o timeout"),
		strings.Contains(lower, "tls:"), strings.Contains(lower, "dial tcp"):
		return failure{"provider_unreachable", "couldn't reach " + providerLabel(provider) + ": " + detail,
			"check this machine's internet connection, proxy and firewall", ""}
	case strings.Contains(msg, "GitHub /user"):
		return failure{"github_api_error", "GitHub's API refused the access token: " + detail,
			"the OAuth app needs the read:user and user:email scopes (gorbital asks for them); a GitHub App needs the Email addresses permission", LinkGuide}
	case strings.Contains(lower, "failed to verify signature") || strings.Contains(lower, "failed to verify id token signature"):
		return failure{"id_token_signature", "the ID token's signature doesn't verify with " + providerLabel(provider) + "'s published keys: " + detail,
			"the token wasn't issued by " + providerLabel(provider) + " (or was changed); get a new one from the provider's SDK", ""}
	case strings.Contains(lower, "token is expired"):
		return failure{"token_expired", "the ID token has expired: " + detail,
			"ID tokens live an hour and sign-in accepts them for " + social.MaxTokenAge.String() + " after issue; get a new one. If it was just issued, this computer's clock is behind: run the network check", ""}
	case strings.Contains(msg, "older than") || strings.Contains(lower, "issued in the future") || strings.Contains(lower, "before issued"):
		return failure{"clock_skew", "the token's issue time is outside what sign-in accepts: " + detail,
			"a token must be under " + social.MaxTokenAge.String() + " old; set this computer's clock to automatic time, then run the network check to see the difference", ""}
	case strings.Contains(msg, "isn't a configured client"):
		return failure{"audience_mismatch", "the token was issued for another client: " + detail, audienceFix(provider), LinkEnvironment}
	case strings.Contains(msg, "issuer"):
		return failure{"issuer_mismatch", "the token's issuer isn't " + providerLabel(provider) + ": " + detail, "use a token from " + providerLabel(provider) + "'s SDK for this provider", ""}
	case strings.Contains(msg, "nonce doesn't match"):
		return failure{"nonce_mismatch", "the token's nonce doesn't match the one sent", nonceFix(provider), ""}
	case errors.Is(err, social.ErrInvalidToken):
		return failure{"invalid_token", "the ID token doesn't verify: " + detail, "get a new token from the provider's SDK", ""}
	case errors.Is(err, social.ErrExchange):
		return failure{"provider_error", providerLabel(provider) + " refused the exchange: " + detail, "check the client ID, secret and redirect URI " + callback, LinkGuide}
	}
	return failure{"provider_error", detail, "", ""}
}

// clientSecrets are the configured secrets, which never appear in a
// message.
func (t *Tester) clientSecrets() []string {
	a := t.cfg.App.Auth
	return []string{a.GoogleClientSecret.Reveal(), a.GitHubClientSecret.Reveal()}
}

// describeFailure explains an OAuth error code a provider returned, on the
// callback or from its token endpoint.
func describeFailure(provider, code, detail, callback string) failure {
	label := providerLabel(provider)
	with := func(msg string) string {
		if detail != "" {
			return msg + ": " + detail
		}
		return msg
	}
	switch code {
	case "access_denied", "user_cancelled_authorize", "user_cancelled_login":
		return failure{"access_denied", with("the sign-in was cancelled at " + label), "try again and allow access; nothing is wrong with the configuration so far", ""}
	case "redirect_uri_mismatch":
		return failure{"redirect_uri_mismatch", with(label + " doesn't have the app's callback URL registered for this client"),
			"register exactly " + callback + " as " + callbackField(provider) + " (scheme, host, port and path must match), or change APP_PUBLIC_URL", LinkGuide}
	case "invalid_client", "unauthorized_client", "incorrect_client_credentials":
		return failure{"invalid_client", with(label + " doesn't accept the client credentials"), clientFix(provider), LinkEnvironment}
	case "invalid_grant", "bad_verification_code":
		return failure{"invalid_grant", with(label + " refused the authorization code"),
			"the code was used, expired, or issued for another redirect URI or PKCE verifier; start the test again, and check " + callback + " is the registered " + callbackField(provider), ""}
	case "invalid_request", "invalid_scope", "unsupported_response_type":
		return failure{"invalid_request", with(label + " refused the request (" + code + ")"),
			"check the client ID is " + label + "'s web client and the redirect URI " + callback + " is registered", LinkGuide}
	case "application_suspended":
		return failure{"invalid_client", with("GitHub has suspended the OAuth app"), "check the OAuth app in GitHub's developer settings", ""}
	}
	return failure{"provider_error", with(label + " returned " + code), "see " + label + "'s error description and the sign-in guide", LinkGuide}
}

func callbackField(provider string) string {
	switch provider {
	case social.Google:
		return "an Authorized redirect URI of the Web application client (Google Cloud console → APIs & Services → Credentials)"
	case social.Apple:
		return "a Return URL of the Services ID (Apple Developer → Identifiers → Services IDs → Sign in with Apple → Configure)"
	case social.GitHub:
		return "the Authorization callback URL of the OAuth app (GitHub → Settings → Developer settings → OAuth Apps)"
	}
	return "the redirect URI"
}

func clientFix(provider string) string {
	switch provider {
	case social.Google:
		return "check GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET are the Web application client's (not an iOS or Android client), and the secret wasn't reset or deleted"
	case social.Apple:
		return "check APPLE_TEAM_ID, APPLE_KEY_ID and the .p8 key (APPLE_PRIVATE_KEY_FILE) belong together, the key is enabled for Sign in with Apple, " +
			"and APPLE_SERVICES_ID is the Services ID (not the App ID)"
	case social.GitHub:
		return "check GITHUB_CLIENT_ID and GITHUB_CLIENT_SECRET are the OAuth app's, and the secret wasn't regenerated"
	}
	return "check the client ID and secret"
}

func audienceFix(provider string) string {
	switch provider {
	case social.Google:
		return "add the client ID the app's SDK uses: GOOGLE_IOS_CLIENT_ID or GOOGLE_ANDROID_CLIENT_ID (Android apps use the Web client ID as serverClientId)"
	case social.Apple:
		return "add the iOS app's bundle ID to APPLE_BUNDLE_IDS (web tokens are issued for APPLE_SERVICES_ID)"
	}
	return "check the client IDs"
}

func nonceFix(provider string) string {
	if provider == social.Apple {
		return "send the nonce from POST /v1/auth/apple/nonce as is, and put its SHA-256 in hex in the Apple request"
	}
	return "put the nonce from POST /v1/auth/google/nonce in the Google request as is, and send the same value"
}
