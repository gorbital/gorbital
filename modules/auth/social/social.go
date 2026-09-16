// Package social signs people in with Google, Apple (ADR-0046) and GitHub
// (ADR-0059): the web authorization code flow with state, nonce and PKCE, ID
// tokens from native apps checked against the app's client IDs, Apple's
// client secret, token revocation and server-to-server notifications, and
// GitHub's user and email API for a provider without OpenID Connect. It wraps
// golang.org/x/oauth2 and github.com/coreos/go-oidc, so apps never import
// their types.
//
// The app keeps the state, nonce and PKCE verifier of a web sign-in on the
// server, and resolves the returned Identity to an account.
//
// Stability: stable (ADR-0015, ADR-0054).
package social

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// Provider names.
const (
	Google = "google"
	Apple  = "apple"
	GitHub = "github"
)

const (
	// MaxTokenAge is how old an ID token can be when it's verified.
	MaxTokenAge = 10 * time.Minute
	// clockSkew tolerates clocks that disagree slightly.
	clockSkew = time.Minute
)

// Errors returned by the package. Check them with [errors.Is].
var (
	// ErrInvalidConfig reports a configuration that can't be used.
	ErrInvalidConfig = errors.New("social: invalid configuration")
	// ErrInvalidToken reports an ID token or notification that fails
	// verification: signature, issuer, audience, nonce, age or claims.
	ErrInvalidToken = errors.New("social: invalid ID token")
	// ErrExchange reports an authorization code the provider refused, or a
	// provider that couldn't be reached.
	ErrExchange = errors.New("social: authorization code exchange failed")
	// ErrWebUnavailable reports a provider configured only for native apps.
	ErrWebUnavailable = errors.New("social: web sign-in is not configured")
	// ErrNotSupported reports an operation the provider doesn't offer, such
	// as verifying an ID token from GitHub, which issues none.
	ErrNotSupported = errors.New("social: not supported by this provider")
)

// Endpoints are a provider's URLs. Zero values use Google's, Apple's or
// GitHub's; tests point them at socialtest.
type Endpoints struct {
	AuthURL   string
	TokenURL  string
	KeysURL   string
	RevokeURL string
	// Issuers are the accepted "iss" values of ID tokens.
	Issuers []string
	// APIURL is the base URL of GitHub's REST API, which returns the person
	// and their email addresses; unused by other providers.
	APIURL string
}

// GoogleEndpoints returns Google's endpoints.
func GoogleEndpoints() Endpoints {
	return Endpoints{ //nolint:gosec // public URLs, not credentials
		AuthURL:   "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:  "https://oauth2.googleapis.com/token",
		KeysURL:   "https://www.googleapis.com/oauth2/v3/certs",
		RevokeURL: "https://oauth2.googleapis.com/revoke",
		Issuers:   []string{"https://accounts.google.com", "accounts.google.com"},
	}
}

// AppleEndpoints returns Apple's endpoints.
func AppleEndpoints() Endpoints {
	return Endpoints{ //nolint:gosec // public URLs, not credentials
		AuthURL:   "https://appleid.apple.com/auth/authorize",
		TokenURL:  "https://appleid.apple.com/auth/token",
		KeysURL:   "https://appleid.apple.com/auth/keys",
		RevokeURL: "https://appleid.apple.com/auth/revoke",
		Issuers:   []string{"https://appleid.apple.com"},
	}
}

// Identity is a person as a verified ID token, or GitHub's API, describes
// them.
type Identity struct {
	// Provider is Google, Apple or GitHub.
	Provider string
	// Subject identifies the person at the provider; it never changes. For
	// GitHub it is the numeric user ID in decimal, not the login, which
	// people can rename.
	Subject string
	Email   string
	// EmailVerified reports that the provider checked the person owns Email.
	EmailVerified bool
	// PrivateEmail reports an Apple relay address.
	PrivateEmail bool
	// HostedDomain is the Google Workspace domain of the account ("hd"),
	// empty for personal Google accounts and Apple.
	HostedDomain string
	// Name is the person's name, when the provider sends it; for GitHub, the
	// login when the profile has no name.
	Name string
	// Audience is the client ID the token was issued for (for GitHub, the
	// OAuth app's client ID).
	Audience string
}

// AuthoritativeEmail reports whether the provider manages Email's domain,
// so a verified Email still belongs to the person: Google for gmail.com,
// googlemail.com and the account's own Workspace domain, Apple for relay
// addresses and iCloud (icloud.com, me.com, mac.com). For other addresses,
// EmailVerified only says the person controlled the address when they
// added it to their provider account, possibly years ago; don't link such
// an identity to an existing account without the account's owner (security
// review AUTH-M-1). GitHub hosts no one's email, so it is never
// authoritative (ADR-0059).
func (id Identity) AuthoritativeEmail() bool {
	if !id.EmailVerified {
		return false
	}
	at := strings.LastIndexByte(id.Email, '@')
	if at < 0 {
		return false
	}
	domain := strings.ToLower(id.Email[at+1:])
	switch id.Provider {
	case Google:
		return domain == "gmail.com" || domain == "googlemail.com" ||
			(id.HostedDomain != "" && strings.EqualFold(id.HostedDomain, domain))
	case Apple:
		return id.PrivateEmail || slices.Contains([]string{"privaterelay.appleid.com", "icloud.com", "me.com", "mac.com"}, domain)
	}
	return false
}

// Token is the result of a code exchange.
type Token struct {
	Identity Identity
	// RefreshToken is set when the provider returns one (Apple); store it
	// encrypted to revoke it later.
	RefreshToken string
}

// GoogleConfig configures Google sign-in.
type GoogleConfig struct {
	// ClientID and ClientSecret are the Web application client. Android apps
	// use ClientID too.
	ClientID     string
	ClientSecret string
	// NativeClientIDs are the iOS and Android client IDs ID tokens may be
	// issued for.
	NativeClientIDs []string
	Endpoints       Endpoints
	HTTPClient      *http.Client
	Now             func() time.Time
}

// Provider runs one provider's sign-ins. It is safe for concurrent use.
type Provider struct {
	name string
	// webClient is the client ID of the web flow; empty for native only.
	webClient string
	// clients are every client ID ID tokens may be issued for.
	clients []string
	secret  func(clientID string) (string, error)
	ep      Endpoints
	// keys verify ID tokens; nil for GitHub, which has no OpenID Connect.
	keys     *oidc.RemoteKeySet
	client   *http.Client
	now      func() time.Time
	scopes   []string
	pkce     bool
	formPost bool
}

// NewGoogle returns the Google provider. It returns [ErrInvalidConfig].
func NewGoogle(c GoogleConfig) (*Provider, error) {
	if c.ClientID == "" || c.ClientSecret == "" {
		return nil, fmt.Errorf("%w: Google needs the web client ID and secret", ErrInvalidConfig)
	}
	secret := c.ClientSecret
	p := newProvider(Google, withDefaults(c.Endpoints, GoogleEndpoints()), c.HTTPClient, c.Now)
	p.webClient = c.ClientID
	p.clients = append([]string{c.ClientID}, c.NativeClientIDs...)
	p.secret = func(string) (string, error) { return secret, nil }
	p.scopes, p.pkce = []string{"openid", "email", "profile"}, true
	return p, nil
}

func newProvider(name string, ep Endpoints, client *http.Client, now func() time.Time) *Provider {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	if now == nil {
		now = time.Now
	}
	p := &Provider{name: name, ep: ep, client: client, now: now}
	if name != GitHub {
		p.keys = oidc.NewRemoteKeySet(oidc.ClientContext(context.Background(), client), ep.KeysURL)
	}
	return p
}

func withDefaults(ep, def Endpoints) Endpoints {
	if ep.AuthURL == "" && ep.TokenURL == "" && ep.KeysURL == "" && ep.APIURL == "" {
		return def
	}
	return ep
}

// Name returns the provider's name: Google, Apple or GitHub.
func (p *Provider) Name() string { return p.name }

// Web reports whether the web flow is configured.
func (p *Provider) Web() bool { return p.webClient != "" }

// NativeClients returns the client IDs native apps' tokens may be issued for,
// other than the web client.
func (p *Provider) NativeClients() []string {
	return slices.DeleteFunc(slices.Clone(p.clients), func(id string) bool { return id == p.webClient })
}

// NewPKCEVerifier returns a random PKCE code verifier to keep with a web
// sign-in's state.
func NewPKCEVerifier() string { return oauth2.GenerateVerifier() }

// AuthCodeURL returns the provider URL that starts a web sign-in returning to
// redirectURL. state and nonce are single-use random values the app keeps;
// verifier is from [NewPKCEVerifier] (Apple ignores it). GitHub, which issues
// no ID token, ignores nonce: state and PKCE protect its flow.
func (p *Provider) AuthCodeURL(redirectURL, state, nonce, verifier string) string {
	var opts []oauth2.AuthCodeOption
	if p.keys != nil {
		opts = append(opts, oauth2.SetAuthURLParam("nonce", nonce))
	}
	if p.pkce {
		opts = append(opts, oauth2.S256ChallengeOption(verifier))
	}
	if p.formPost {
		// Apple posts the result, which it requires when asking for the name
		// and email.
		opts = append(opts, oauth2.SetAuthURLParam("response_mode", "form_post"))
	}
	return p.oauth(redirectURL, p.webClient, "").AuthCodeURL(state, opts...)
}

// Exchange trades a web sign-in's authorization code for tokens and verifies
// the ID token against the web client and nonce. For GitHub, it reads the
// person from GitHub's API with the access token instead, which it then
// forgets (nonce is unused). It returns [ErrWebUnavailable], [ErrExchange] or
// [ErrInvalidToken].
func (p *Provider) Exchange(ctx context.Context, redirectURL, code, verifier, nonce string) (Token, error) {
	if !p.Web() {
		return Token{}, ErrWebUnavailable
	}
	tok, err := p.exchange(ctx, p.webClient, redirectURL, code, verifier)
	if err != nil {
		return Token{}, err
	}
	if p.name == GitHub {
		id, err := p.gitHubIdentity(ctx, tok.AccessToken)
		return Token{Identity: id}, err
	}
	raw, _ := tok.Extra("id_token").(string)
	id, err := p.verify(ctx, raw, nonce, []string{p.webClient})
	if err != nil {
		return Token{}, err
	}
	return Token{Identity: id, RefreshToken: tok.RefreshToken}, nil
}

// ExchangeNativeCode trades an authorization code a native app received for
// clientID, such as Apple's authorizationCode on iOS, and returns the refresh
// token (empty when the provider sends none). It returns [ErrExchange],
// [ErrInvalidConfig] for a client ID that isn't configured, or
// [ErrNotSupported] for GitHub.
func (p *Provider) ExchangeNativeCode(ctx context.Context, code, clientID string) (string, error) {
	if p.keys == nil {
		return "", fmt.Errorf("%w: %s has no native sign-in", ErrNotSupported, p.name)
	}
	if !slices.Contains(p.clients, clientID) {
		return "", fmt.Errorf("%w: unknown client ID %q", ErrInvalidConfig, clientID)
	}
	tok, err := p.exchange(ctx, clientID, "", code, "")
	if err != nil {
		return "", err
	}
	return tok.RefreshToken, nil
}

func (p *Provider) exchange(ctx context.Context, clientID, redirectURL, code, verifier string) (*oauth2.Token, error) {
	secret, err := p.secret(clientID)
	if err != nil {
		return nil, err
	}
	var opts []oauth2.AuthCodeOption
	if p.pkce && verifier != "" {
		opts = append(opts, oauth2.VerifierOption(verifier))
	}
	tok, err := p.oauth(redirectURL, clientID, secret).Exchange(context.WithValue(ctx, oauth2.HTTPClient, p.client), code, opts...)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrExchange, err) //nolint:errorlint // the library's errors aren't API
	}
	return tok, nil
}

func (p *Provider) oauth(redirectURL, clientID, secret string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: secret,
		RedirectURL:  redirectURL,
		Scopes:       p.scopes,
		Endpoint:     oauth2.Endpoint{AuthURL: p.ep.AuthURL, TokenURL: p.ep.TokenURL, AuthStyle: oauth2.AuthStyleInParams},
	}
}

// VerifyIDToken verifies an ID token a native app obtained: signature,
// issuer, an audience among the configured client IDs, expiry, an age under
// [MaxTokenAge], and nonce. It returns [ErrInvalidToken], wrapping
// [ErrNotSupported] for GitHub, which issues no ID tokens.
func (p *Provider) VerifyIDToken(ctx context.Context, rawIDToken, nonce string) (Identity, error) {
	return p.verify(ctx, rawIDToken, nonce, p.clients)
}

func (p *Provider) verify(ctx context.Context, raw, nonce string, audiences []string) (Identity, error) {
	if p.keys == nil {
		return Identity{}, fmt.Errorf("%w: %w: %s issues no ID tokens", ErrInvalidToken, ErrNotSupported, p.name)
	}
	if raw == "" || nonce == "" {
		return Identity{}, fmt.Errorf("%w: an ID token and nonce are required", ErrInvalidToken)
	}
	now := p.now()
	verifier := oidc.NewVerifier("", p.keys, &oidc.Config{
		SkipClientIDCheck: true, // checked below against several client IDs
		SkipIssuerCheck:   true, // checked below against Endpoints.Issuers
		Now:               func() time.Time { return now.Add(-clockSkew) },
	})
	t, err := verifier.Verify(ctx, raw)
	if err != nil {
		return Identity{}, invalid(err)
	}
	aud := sharedAudience(t.Audience, audiences)
	switch {
	case !slices.Contains(p.ep.Issuers, t.Issuer):
		return Identity{}, fmt.Errorf("%w: issuer %q", ErrInvalidToken, t.Issuer)
	case aud == "":
		return Identity{}, fmt.Errorf("%w: audience %q isn't a configured client", ErrInvalidToken, t.Audience)
	case subtle.ConstantTimeCompare([]byte(t.Nonce), []byte(nonce)) != 1:
		return Identity{}, fmt.Errorf("%w: nonce doesn't match", ErrInvalidToken)
	case t.IssuedAt.After(now.Add(clockSkew)) || now.Sub(t.IssuedAt) > MaxTokenAge:
		return Identity{}, fmt.Errorf("%w: issued at %s, older than %s", ErrInvalidToken, t.IssuedAt, MaxTokenAge)
	case t.Subject == "":
		return Identity{}, fmt.Errorf("%w: no subject", ErrInvalidToken)
	}
	var claims struct {
		Email         string   `json:"email"`
		EmailVerified flexBool `json:"email_verified"`
		PrivateEmail  flexBool `json:"is_private_email"`
		HostedDomain  string   `json:"hd"`
		Name          string   `json:"name"`
	}
	if err := t.Claims(&claims); err != nil {
		return Identity{}, invalid(err)
	}
	id := Identity{
		Provider: p.name, Subject: t.Subject, Email: strings.TrimSpace(claims.Email), EmailVerified: bool(claims.EmailVerified),
		PrivateEmail: bool(claims.PrivateEmail), Name: strings.TrimSpace(claims.Name), Audience: aud,
	}
	if p.name == Google {
		id.HostedDomain = strings.TrimSpace(claims.HostedDomain)
	}
	return id, nil
}

// Revoke revokes a refresh token issued for clientID, as Apple requires when
// an account is deleted. It returns [ErrNotSupported] for GitHub, whose
// tokens are never kept.
func (p *Provider) Revoke(ctx context.Context, refreshToken, clientID string) error {
	if p.ep.RevokeURL == "" {
		return fmt.Errorf("%w: %s has no token revocation", ErrNotSupported, p.name)
	}
	secret, err := p.secret(clientID)
	if err != nil {
		return err
	}
	form := url.Values{"client_id": {clientID}, "client_secret": {secret}, "token": {refreshToken}, "token_type_hint": {"refresh_token"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.ep.RevokeURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("social: revoke token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("social: revoke token: %s", resp.Status)
	}
	return nil
}

func sharedAudience(got, allowed []string) string {
	for _, a := range got {
		if slices.Contains(allowed, a) {
			return a
		}
	}
	return ""
}

func invalid(err error) error {
	return fmt.Errorf("%w: %v", ErrInvalidToken, err) //nolint:errorlint // the library's errors aren't API
}

// flexBool reads a boolean sent as true or "true": Apple sends strings.
type flexBool bool

func (b *flexBool) UnmarshalJSON(data []byte) error {
	switch string(data) {
	case `true`, `"true"`:
		*b = true
	case `false`, `"false"`, `null`:
		*b = false
	default:
		return fmt.Errorf("social: %s is not a boolean", data)
	}
	return nil
}
