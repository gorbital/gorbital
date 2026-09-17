# modules/auth/social

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/modules/auth/social"
```

Package social signs people in with Google, Apple (ADR-0046) and GitHub (ADR-0059): the web authorization code flow with state, nonce and PKCE, ID tokens from native apps checked against the app's client IDs, Apple's client secret, token revocation and server-to-server notifications, and GitHub's user and email API for a provider without OpenID Connect. It wraps golang.org/x/oauth2 and github.com/coreos/go-oidc, so apps never import their types.

The app keeps the state, nonce and PKCE verifier of a web sign-in on the server, and resolves the returned Identity to an account.

Stability: stable (ADR-0015, ADR-0054).

## Contents

- Constants: [`NotificationEmailDisabled`](#NotificationEmailDisabled), [`NotificationEmailEnabled`](#NotificationEmailEnabled), [`NotificationConsentRevoked`](#NotificationConsentRevoked), [`NotificationAccountDelete`](#NotificationAccountDelete), [`Google`](#Google), [`Apple`](#Apple), [`GitHub`](#GitHub), [`MaxNotificationAge`](#MaxNotificationAge), [`MaxTokenAge`](#MaxTokenAge)
- Variables: [`ErrInvalidConfig`](#ErrInvalidConfig), [`ErrInvalidToken`](#ErrInvalidToken), [`ErrExchange`](#ErrExchange), [`ErrWebUnavailable`](#ErrWebUnavailable), [`ErrNotSupported`](#ErrNotSupported)
- Functions: [`NewPKCEVerifier`](#NewPKCEVerifier), [`ParseApplePrivateKey`](#ParseApplePrivateKey)
- Types:
  - [`AppleConfig`](#AppleConfig)
  - [`Endpoints`](#Endpoints): [`AppleEndpoints`](#AppleEndpoints), [`GitHubEndpoints`](#GitHubEndpoints), [`GoogleEndpoints`](#GoogleEndpoints)
  - [`GitHubConfig`](#GitHubConfig)
  - [`GoogleConfig`](#GoogleConfig)
  - [`Identity`](#Identity): [`Identity.AuthoritativeEmail`](#Identity.AuthoritativeEmail)
  - [`Notification`](#Notification)
  - [`Provider`](#Provider): [`NewApple`](#NewApple), [`NewGitHub`](#NewGitHub), [`NewGoogle`](#NewGoogle), [`Provider.AppleNotification`](#Provider.AppleNotification), [`Provider.AuthCodeURL`](#Provider.AuthCodeURL), [`Provider.Exchange`](#Provider.Exchange), [`Provider.ExchangeNativeCode`](#Provider.ExchangeNativeCode), [`Provider.Name`](#Provider.Name), [`Provider.NativeClients`](#Provider.NativeClients), [`Provider.Revoke`](#Provider.Revoke), [`Provider.VerifyIDToken`](#Provider.VerifyIDToken), [`Provider.Web`](#Provider.Web)
  - [`Token`](#Token)

## Constants

<a id="NotificationEmailDisabled"></a>
<a id="NotificationEmailEnabled"></a>
<a id="NotificationConsentRevoked"></a>
<a id="NotificationAccountDelete"></a>

```go
const (
	NotificationEmailDisabled  = "email-disabled"
	NotificationEmailEnabled   = "email-enabled"
	NotificationConsentRevoked = "consent-revoked"
	NotificationAccountDelete  = "account-delete"
)
```

Apple server-to-server notification types.

*Since `v0.1.0`*

<a id="Google"></a>
<a id="Apple"></a>
<a id="GitHub"></a>

```go
const (
	Google = "google"
	Apple  = "apple"
	GitHub = "github"
)
```

Provider names.

*Since `v0.1.0`*

<a id="MaxNotificationAge"></a>

```go
const MaxNotificationAge = time.Hour
```

MaxNotificationAge is how old an Apple notification can be when it's verified, so an old payload can't be replayed later.

*Since `v0.1.0`*

<a id="MaxTokenAge"></a>

```go
const (
	// MaxTokenAge is how old an ID token can be when it's verified.
	MaxTokenAge = 10 * time.Minute
)
```

*Since `v0.1.0`*

## Variables

<a id="ErrInvalidConfig"></a>
<a id="ErrInvalidToken"></a>
<a id="ErrExchange"></a>
<a id="ErrWebUnavailable"></a>
<a id="ErrNotSupported"></a>

```go
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
```

Errors returned by the package. Check them with [errors.Is](https://pkg.go.dev/errors#Is).

*Since `v0.1.0`*

## Functions

<a id="NewPKCEVerifier"></a>

### func NewPKCEVerifier

```go
func NewPKCEVerifier() string
```

NewPKCEVerifier returns a random PKCE code verifier to keep with a web sign-in's state.

*Since `v0.1.0`*

<a id="ParseApplePrivateKey"></a>

### func ParseApplePrivateKey

```go
func ParseApplePrivateKey(pemBytes []byte) (*ecdsa.PrivateKey, error)
```

ParseApplePrivateKey reads a Sign in with Apple .p8 key: a PEM PKCS #8 P-256 private key. It returns [ErrInvalidConfig](#ErrInvalidConfig).

*Since `v0.1.0`*

## Types

<a id="AppleConfig"></a>
<a id="AppleConfig.TeamID"></a>
<a id="AppleConfig.KeyID"></a>
<a id="AppleConfig.PrivateKey"></a>
<a id="AppleConfig.ServicesID"></a>
<a id="AppleConfig.BundleIDs"></a>
<a id="AppleConfig.Endpoints"></a>
<a id="AppleConfig.HTTPClient"></a>
<a id="AppleConfig.Now"></a>

### type AppleConfig

```go
type AppleConfig struct {
	// TeamID, KeyID and PrivateKey sign client secrets: the Team ID, and the
	// ID and .p8 key of a Sign in with Apple key.
	TeamID     string
	KeyID      string
	PrivateKey *ecdsa.PrivateKey
	// ServicesID is the web client; empty for native apps only.
	ServicesID string
	// BundleIDs are the iOS apps ID tokens may be issued for.
	BundleIDs  []string
	Endpoints  Endpoints
	HTTPClient *http.Client
	Now        func() time.Time
}
```

AppleConfig configures Sign in with Apple.

*Since `v0.1.0`*

<a id="Endpoints"></a>
<a id="Endpoints.AuthURL"></a>
<a id="Endpoints.TokenURL"></a>
<a id="Endpoints.KeysURL"></a>
<a id="Endpoints.RevokeURL"></a>
<a id="Endpoints.Issuers"></a>
<a id="Endpoints.APIURL"></a>

### type Endpoints

```go
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
```

Endpoints are a provider's URLs. Zero values use Google's, Apple's or GitHub's; tests point them at socialtest.

*Since `v0.1.0`*

<a id="AppleEndpoints"></a>

#### func AppleEndpoints

```go
func AppleEndpoints() Endpoints
```

AppleEndpoints returns Apple's endpoints.

*Since `v0.1.0`*

<a id="GitHubEndpoints"></a>

#### func GitHubEndpoints

```go
func GitHubEndpoints() Endpoints
```

GitHubEndpoints returns GitHub's endpoints.

*Since `v0.1.0`*

<a id="GoogleEndpoints"></a>

#### func GoogleEndpoints

```go
func GoogleEndpoints() Endpoints
```

GoogleEndpoints returns Google's endpoints.

*Since `v0.1.0`*

<a id="GitHubConfig"></a>
<a id="GitHubConfig.ClientID"></a>
<a id="GitHubConfig.ClientSecret"></a>
<a id="GitHubConfig.Endpoints"></a>
<a id="GitHubConfig.HTTPClient"></a>

### type GitHubConfig

```go
type GitHubConfig struct {
	// ClientID and ClientSecret are the OAuth app's.
	ClientID     string
	ClientSecret string
	Endpoints    Endpoints
	HTTPClient   *http.Client
}
```

GitHubConfig configures GitHub sign-in with an OAuth app.

*Since `v0.1.0`*

<a id="GoogleConfig"></a>
<a id="GoogleConfig.ClientID"></a>
<a id="GoogleConfig.ClientSecret"></a>
<a id="GoogleConfig.NativeClientIDs"></a>
<a id="GoogleConfig.Endpoints"></a>
<a id="GoogleConfig.HTTPClient"></a>
<a id="GoogleConfig.Now"></a>

### type GoogleConfig

```go
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
```

GoogleConfig configures Google sign-in.

*Since `v0.1.0`*

<a id="Identity"></a>
<a id="Identity.Provider"></a>
<a id="Identity.Subject"></a>
<a id="Identity.Email"></a>
<a id="Identity.EmailVerified"></a>
<a id="Identity.PrivateEmail"></a>
<a id="Identity.HostedDomain"></a>
<a id="Identity.Name"></a>
<a id="Identity.Audience"></a>

### type Identity

```go
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
```

Identity is a person as a verified ID token, or GitHub's API, describes them.

*Since `v0.1.0`*

<a id="Identity.AuthoritativeEmail"></a>

#### func (Identity) AuthoritativeEmail

```go
func (id Identity) AuthoritativeEmail() bool
```

AuthoritativeEmail reports whether the provider manages Email's domain, so a verified Email still belongs to the person: Google for gmail.com, googlemail.com and the account's own Workspace domain, Apple for relay addresses and iCloud (icloud.com, me.com, mac.com). For other addresses, EmailVerified only says the person controlled the address when they added it to their provider account, possibly years ago; don't link such an identity to an existing account without the account's owner (security review AUTH-M-1). GitHub hosts no one's email, so it is never authoritative (ADR-0059).

*Since `v0.1.0`*

<a id="Notification"></a>
<a id="Notification.ID"></a>
<a id="Notification.Type"></a>
<a id="Notification.Subject"></a>
<a id="Notification.Email"></a>
<a id="Notification.PrivateEmail"></a>
<a id="Notification.At"></a>
<a id="Notification.IssuedAt"></a>

### type Notification

```go
type Notification struct {
	// ID identifies the notification ("jti", or a hash of the payload when
	// Apple sends none). Remember it until IssuedAt plus
	// [MaxNotificationAge] and ignore a notification seen before: a replayed
	// payload carries the same ID.
	ID string
	// Type is one of the Notification constants.
	Type    string
	Subject string
	Email   string
	// PrivateEmail reports an Apple relay address.
	PrivateEmail bool
	// At is when the event happened; IssuedAt when Apple signed the
	// notification.
	At       time.Time
	IssuedAt time.Time
}
```

Notification is an Apple server-to-server notification about a person who signed in with Apple.

*Since `v0.1.0`*

<a id="Provider"></a>

### type Provider

```go
type Provider struct {
	// contains filtered or unexported fields
}
```

Provider runs one provider's sign-ins. It is safe for concurrent use.

*Since `v0.1.0`*

<a id="NewApple"></a>

#### func NewApple

```go
func NewApple(c AppleConfig) (*Provider, error)
```

NewApple returns the Apple provider. It returns [ErrInvalidConfig](#ErrInvalidConfig).

*Since `v0.1.0`*

<a id="NewGitHub"></a>

#### func NewGitHub

```go
func NewGitHub(c GitHubConfig) (*Provider, error)
```

NewGitHub returns the GitHub provider (ADR-0059). GitHub has no OpenID Connect: [Provider.Exchange](#Provider.Exchange) trades the code (with state and PKCE, like Google) for an access token, reads the person from GET /user and their primary email from GET /user/emails with the scopes read:user and user:email, and forgets the token. It offers the web flow only: ID tokens, native codes and revocation return [ErrNotSupported](#ErrNotSupported). It returns [ErrInvalidConfig](#ErrInvalidConfig).

*Since `v0.1.0`*

<a id="NewGoogle"></a>

#### func NewGoogle

```go
func NewGoogle(c GoogleConfig) (*Provider, error)
```

NewGoogle returns the Google provider. It returns [ErrInvalidConfig](#ErrInvalidConfig).

*Since `v0.1.0`*

<a id="Provider.AppleNotification"></a>

#### func (*Provider) AppleNotification

```go
func (p *Provider) AppleNotification(ctx context.Context, payload string) (Notification, error)
```

AppleNotification verifies the payload Apple posts to the notification endpoint: Apple's signature, issuer, a configured audience, and an issue time within [MaxNotificationAge](#MaxNotificationAge). It returns [ErrInvalidToken](#ErrInvalidToken), or [ErrInvalidConfig](#ErrInvalidConfig) on a provider other than Apple.

*Since `v0.1.0`*

<a id="Provider.AuthCodeURL"></a>

#### func (*Provider) AuthCodeURL

```go
func (p *Provider) AuthCodeURL(redirectURL, state, nonce, verifier string) string
```

AuthCodeURL returns the provider URL that starts a web sign-in returning to redirectURL. state and nonce are single-use random values the app keeps; verifier is from [NewPKCEVerifier](#NewPKCEVerifier) (Apple ignores it). GitHub, which issues no ID token, ignores nonce: state and PKCE protect its flow.

*Since `v0.1.0`*

<a id="Provider.Exchange"></a>

#### func (*Provider) Exchange

```go
func (p *Provider) Exchange(ctx context.Context, redirectURL, code, verifier, nonce string) (Token, error)
```

Exchange trades a web sign-in's authorization code for tokens and verifies the ID token against the web client and nonce. For GitHub, it reads the person from GitHub's API with the access token instead, which it then forgets (nonce is unused). It returns [ErrWebUnavailable](#ErrWebUnavailable), [ErrExchange](#ErrExchange) or [ErrInvalidToken](#ErrInvalidToken).

*Since `v0.1.0`*

<a id="Provider.ExchangeNativeCode"></a>

#### func (*Provider) ExchangeNativeCode

```go
func (p *Provider) ExchangeNativeCode(ctx context.Context, code, clientID string) (string, error)
```

ExchangeNativeCode trades an authorization code a native app received for clientID, such as Apple's authorizationCode on iOS, and returns the refresh token (empty when the provider sends none). It returns [ErrExchange](#ErrExchange), [ErrInvalidConfig](#ErrInvalidConfig) for a client ID that isn't configured, or [ErrNotSupported](#ErrNotSupported) for GitHub.

*Since `v0.1.0`*

<a id="Provider.Name"></a>

#### func (*Provider) Name

```go
func (p *Provider) Name() string
```

Name returns the provider's name: Google, Apple or GitHub.

*Since `v0.1.0`*

<a id="Provider.NativeClients"></a>

#### func (*Provider) NativeClients

```go
func (p *Provider) NativeClients() []string
```

NativeClients returns the client IDs native apps' tokens may be issued for, other than the web client.

*Since `v0.1.0`*

<a id="Provider.Revoke"></a>

#### func (*Provider) Revoke

```go
func (p *Provider) Revoke(ctx context.Context, refreshToken, clientID string) error
```

Revoke revokes a refresh token issued for clientID, as Apple requires when an account is deleted. It returns [ErrNotSupported](#ErrNotSupported) for GitHub, whose tokens are never kept.

*Since `v0.1.0`*

<a id="Provider.VerifyIDToken"></a>

#### func (*Provider) VerifyIDToken

```go
func (p *Provider) VerifyIDToken(ctx context.Context, rawIDToken, nonce string) (Identity, error)
```

VerifyIDToken verifies an ID token a native app obtained: signature, issuer, an audience among the configured client IDs, expiry, an age under [MaxTokenAge](#MaxTokenAge), and nonce. It returns [ErrInvalidToken](#ErrInvalidToken), wrapping [ErrNotSupported](#ErrNotSupported) for GitHub, which issues no ID tokens.

*Since `v0.1.0`*

<a id="Provider.Web"></a>

#### func (*Provider) Web

```go
func (p *Provider) Web() bool
```

Web reports whether the web flow is configured.

*Since `v0.1.0`*

<a id="Token"></a>
<a id="Token.Identity"></a>
<a id="Token.RefreshToken"></a>

### type Token

```go
type Token struct {
	Identity Identity
	// RefreshToken is set when the provider returns one (Apple); store it
	// encrypted to revoke it later.
	RefreshToken string
}
```

Token is the result of a code exchange.

*Since `v0.1.0`*
