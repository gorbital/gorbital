# modules/jwt

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/modules/jwt"
```

Package jwt authenticates requests carrying JSON Web Tokens from an external identity provider, such as Auth0, Clerk, Supabase, Firebase or Amazon Cognito (ADR-0085). It verifies the token's signature with the provider's published keys (JWKS), its issuer, audience and validity times, and puts the caller in the request context as an actor.

```go
idp, err := jwt.New(ctx, jwt.Config{
	Issuer:    "https://example.eu.auth0.com/",
	Audiences: []string{"https://api.example.com"},
	JWKSURL:   "https://example.eu.auth0.com/.well-known/jwks.json",
})
handler := idp.Middleware(logger)(mux)
```

Keys are fetched when the authenticator is created, cached as long as the provider's Cache-Control allows (between 5 minutes and 24 hours), and fetched again when a token names a key the cache doesn't have, at most once every 30 seconds. Nothing runs in the background.

Stability: experimental until v0.2.0 (ADR-0015, ADR-0085).

## When to use

When users sign in with an external identity provider (Auth0, Clerk, Supabase, Firebase, Cognito) and your API receives its access tokens. For gorbital's own sessions and API keys, use [modules/auth](modules-auth.md) instead; both can run in the same chain.

- [Security layers](../guides/security-layers.md#external-identity-providers-jwt): provider settings, claims to actors, key caching and outages.

## Contents

- Constants: [`DefaultClockSkew`](#DefaultClockSkew), [`MaxClockSkew`](#MaxClockSkew), [`DefaultPermissionsClaim`](#DefaultPermissionsClaim), [`MaxTokenBytes`](#MaxTokenBytes), [`ReservedPermissionPrefix`](#ReservedPermissionPrefix)
- Variables: [`ErrInvalidToken`](#ErrInvalidToken), [`ErrKeysUnavailable`](#ErrKeysUnavailable), [`DefaultAlgorithms`](#DefaultAlgorithms), [`ErrNoClaim`](#ErrNoClaim)
- Types:
  - [`Authenticator`](#Authenticator): [`New`](#New), [`Authenticator.Middleware`](#Authenticator.Middleware), [`Authenticator.Verify`](#Authenticator.Verify)
  - [`Claims`](#Claims): [`Claims.Decode`](#Claims.Decode), [`Claims.String`](#Claims.String), [`Claims.Strings`](#Claims.Strings)
  - [`Config`](#Config)
  - [`Option`](#Option): [`WithClock`](#WithClock), [`WithHMACSecret`](#WithHMACSecret), [`WithHTTPClient`](#WithHTTPClient)

## Constants

<a id="DefaultClockSkew"></a>
<a id="MaxClockSkew"></a>
<a id="DefaultPermissionsClaim"></a>
<a id="MaxTokenBytes"></a>

```go
const (
	// DefaultClockSkew is how far token times may be from this server's
	// clock unless [Config.ClockSkew] sets another.
	DefaultClockSkew = 30 * time.Second
	// MaxClockSkew is the largest [Config.ClockSkew] accepted.
	MaxClockSkew = 5 * time.Minute
	// DefaultPermissionsClaim is the claim permissions are read from unless
	// [Config.PermissionsClaim] sets another: Auth0's RBAC claim.
	DefaultPermissionsClaim = "permissions"
	// MaxTokenBytes is the longest token accepted.
	MaxTokenBytes = 16 << 10
)
```

Defaults and bounds of [Config](#Config).

*Since `v0.3.0 (unreleased)`*

<a id="ReservedPermissionPrefix"></a>

```go
const ReservedPermissionPrefix = "ops."
```

ReservedPermissionPrefix is the permission namespace of the framework's own operations console (gorbital.dev/gorbital/opshttp). The default actor mapping drops permissions under it, because /ops authorizes on the actor's permissions alone and the claim a provider puts them in is often not fully under the operator's control: an OAuth "scope" claim is asked for by the client, and several providers map user-editable metadata into it. An app that does grant its operators /ops through the provider says so with [Config.ActorFrom](#Config.ActorFrom), which this never touches.

*Since `v0.3.0 (unreleased)`*

## Variables

<a id="ErrInvalidToken"></a>
<a id="ErrKeysUnavailable"></a>

```go
var (
	// ErrInvalidToken reports a token that isn't a signed JWT from the
	// provider for this API, or isn't valid now: malformed, signed with an
	// algorithm or key that isn't allowed, tampered with, expired, not yet
	// valid, or for another issuer or audience. The wrapping error says
	// which, for logs; never show it to clients.
	ErrInvalidToken = errors.New("jwt: invalid token")
	// ErrKeysUnavailable reports that the provider's keys couldn't be
	// fetched, so a token signed with a key that isn't cached can't be
	// verified.
	ErrKeysUnavailable = errors.New("jwt: signing keys unavailable")
)
```

Errors returned by [Authenticator.Verify](#Authenticator.Verify). Check them with [errors.Is](https://pkg.go.dev/errors#Is).

*Since `v0.3.0 (unreleased)`*

<a id="DefaultAlgorithms"></a>

```go
var DefaultAlgorithms = []string{"RS256", "ES256", "EdDSA"}
```

DefaultAlgorithms are the signature algorithms accepted unless [Config.Algorithms](#Config.Algorithms) sets others.

*Since `v0.3.0 (unreleased)`*

<a id="ErrNoClaim"></a>

```go
var ErrNoClaim = errors.New("jwt: no such claim")
```

ErrNoClaim reports a claim the token doesn't carry.

*Since `v0.3.0 (unreleased)`*

## Types

<a id="Authenticator"></a>

### type Authenticator

```go
type Authenticator struct {
	// contains filtered or unexported fields
}
```

An Authenticator verifies tokens from one provider. Create it with [New](#New). It is safe for concurrent use.

*Since `v0.3.0 (unreleased)`*

**Example**

```go
idp := newProvider()
defer idp.srv.Close()
auth, err := jwt.New(context.Background(), jwt.Config{
	Issuer:    "https://idp.example.com/",
	Audiences: []string{"https://api.example.com"},
	JWKSURL:   idp.jwksURL(),
})
if err != nil {
	panic(err)
}
// In a gorbital app: gorbital.WithAuth(auth). With net/http:
books := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	if err := actor.Require(r.Context(), "books.book.read"); err != nil {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	_, _ = w.Write([]byte("[]"))
})
handler := auth.Middleware(slog.Default())(books)

req := httptest.NewRequest(http.MethodGet, "/v1/books", nil)
req.Header.Set("Authorization", "Bearer "+idp.token(map[string]any{"sub": "usr_1", "permissions": []string{"books.book.read"}}))
rec := httptest.NewRecorder()
handler.ServeHTTP(rec, req)
fmt.Println(rec.Code, rec.Body.String())
```

Output:

```text
200 []
```

<a id="New"></a>

#### func New

```go
func New(ctx context.Context, cfg Config, opts ...Option) (*Authenticator, error)
```

New validates cfg and fetches the provider's keys, within ctx. It returns an error for a missing issuer or audience, an unknown or disallowed algorithm, a JWKS URL that isn't https (or http on a loopback address), a clock skew out of bounds, an HMAC secret that is short or not matched by an HS algorithm, or keys that can't be fetched or contain no usable signing key, so a misconfigured app fails when it starts.

*Since `v0.3.0 (unreleased)`*

**Example**

```go
idp := newProvider()
defer idp.srv.Close()

auth, err := jwt.New(context.Background(), jwt.Config{
	Issuer:    "https://idp.example.com/",
	Audiences: []string{"https://api.example.com"},
	JWKSURL:   idp.jwksURL(),
})
fmt.Println(auth != nil, err)

_, err = jwt.New(context.Background(), jwt.Config{
	Issuer:     "https://idp.example.com/",
	Audiences:  []string{"https://api.example.com"},
	JWKSURL:    idp.jwksURL(),
	Algorithms: []string{"none"},
})
fmt.Println(err)
```

Output:

```text
true <nil>
jwt: algorithm "none" is not supported
```

<a id="Authenticator.Middleware"></a>

#### func (*Authenticator) Middleware

```go
func (a *Authenticator) Middleware(logger *slog.Logger) func(http.Handler) http.Handler
```

Middleware returns middleware that authenticates requests carrying a JWT in an "Authorization: Bearer" header and puts the caller in the context as an actor ([actor.With](actor.md#With)), with the client's address and user agent ([actor.WithClient](actor.md#WithClient)) unless earlier middleware set them. Requests without a bearer token, with a token that isn't shaped like a JWT (such as an API key another authenticator handles), or that already have an actor continue unchanged; routes that need a caller answer them 401.

A JWT that fails verification is refused with 401 invalid\_token and a WWW-Authenticate header (RFC 6750), rather than continuing anonymously, so a client learns to get a new token instead of silently losing access. When the provider's keys can't be fetched it answers 503 auth\_unavailable. Refusals are logged at debug level; failed key fetches at warn level, at most once every 30 seconds.

*Since `v0.3.0 (unreleased)`*

**Example**

```go
idp := newProvider()
defer idp.srv.Close()
auth, err := jwt.New(context.Background(), jwt.Config{
	Issuer:    "https://idp.example.com/",
	Audiences: []string{"https://api.example.com"},
	JWKSURL:   idp.jwksURL(),
})
if err != nil {
	panic(err)
}
handler := auth.Middleware(slog.Default())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	a := actor.FromOrAnonymous(r.Context())
	fmt.Fprintf(w, "%s:%s", a.Kind, a.ID)
}))

valid := idp.token(map[string]any{"sub": "usr_1"})
for _, authorization := range []string{
	"Bearer " + valid,
	"", // no token: anonymous
	"Bearer " + valid[:len(valid)-4] + "AAAA", // a tampered JWT: refused
} {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	fmt.Printf("%d %q %s\n", rec.Code, rec.Header().Get("WWW-Authenticate"), rec.Body.String())
}
```

Output:

```text
200 "" user:usr_1
200 "" anonymous:
401 "Bearer error=\"invalid_token\"" {"title":"Unauthorized","status":401,"code":"invalid_token","detail":"the access token is invalid or expired; get a new one"}
```

<a id="Authenticator.Verify"></a>

#### func (*Authenticator) Verify

```go
func (a *Authenticator) Verify(ctx context.Context, token string) (Claims, error)
```

Verify checks token and returns its claims. It returns an error wrapping [ErrInvalidToken](#ErrInvalidToken) when the token isn't acceptable, and [ErrKeysUnavailable](#ErrKeysUnavailable) when its key isn't cached and the provider's keys can't be fetched. Use it outside HTTP middleware, such as for a WebSocket's first message.

*Since `v0.3.0 (unreleased)`*

**Example**

```go
idp := newProvider()
defer idp.srv.Close()
auth, err := jwt.New(context.Background(), jwt.Config{
	Issuer:    "https://idp.example.com/",
	Audiences: []string{"https://api.example.com"},
	JWKSURL:   idp.jwksURL(),
})
if err != nil {
	panic(err)
}
claims, err := auth.Verify(context.Background(), idp.token(map[string]any{"sub": "usr_1"}))
fmt.Println(claims.Subject, err)

expired := idp.token(map[string]any{"sub": "usr_1", "exp": time.Now().Add(-time.Hour).Unix()})
_, err = auth.Verify(context.Background(), expired)
fmt.Println(errors.Is(err, jwt.ErrInvalidToken), err)
```

Output:

```text
usr_1 <nil>
true jwt: invalid token: expired
```

<a id="Claims"></a>
<a id="Claims.Issuer"></a>
<a id="Claims.Subject"></a>
<a id="Claims.Audience"></a>
<a id="Claims.ExpiresAt"></a>
<a id="Claims.NotBefore"></a>
<a id="Claims.IssuedAt"></a>
<a id="Claims.ID"></a>

### type Claims

```go
type Claims struct {
	// Issuer is "iss", equal to [Config.Issuer].
	Issuer string
	// Subject is "sub", the caller's ID at the provider. Never empty.
	Subject string
	// Audience is "aud".
	Audience []string
	// ExpiresAt is "exp"; NotBefore "nbf" and IssuedAt "iat", or zero when
	// the token has none.
	ExpiresAt, NotBefore, IssuedAt time.Time
	// ID is "jti", or empty.
	ID string
	// contains filtered or unexported fields
}
```

Claims are a verified token's claims: the registered ones as fields, and any other through [Claims.String](#Claims.String), [Claims.Strings](#Claims.Strings) and [Claims.Decode](#Claims.Decode).

*Since `v0.3.0 (unreleased)`*

**Example**

```go
idp := newProvider()
defer idp.srv.Close()
auth, err := jwt.New(context.Background(), jwt.Config{Issuer: "https://idp.example.com/", Audiences: []string{"https://api.example.com"}, JWKSURL: idp.jwksURL()})
if err != nil {
	panic(err)
}
c, err := auth.Verify(context.Background(), idp.token(map[string]any{"sub": "usr_1", "jti": "tok_9"}))
if err != nil {
	panic(err)
}
fmt.Println(c.Issuer, c.Subject, c.Audience, c.ID, time.Until(c.ExpiresAt) > 59*time.Minute)
```

Output:

```text
https://idp.example.com/ usr_1 [https://api.example.com] tok_9 true
```

<a id="Claims.Decode"></a>

#### func (Claims) Decode

```go
func (c Claims) Decode(name string, v any) error
```

Decode unmarshals a claim's JSON into v, such as a provider's nested metadata object. It returns an error wrapping [ErrNoClaim](#ErrNoClaim) when the claim is missing, or the JSON error when it doesn't fit v.

*Since `v0.3.0 (unreleased)`*

**Example**

```go
// Supabase puts app metadata in a nested object.
c := verified(map[string]any{"sub": "usr_1", "app_metadata": map[string]any{"provider": "email", "tenant": "org_7"}})
var meta struct {
	Tenant string `json:"tenant"`
}
err := c.Decode("app_metadata", &meta)
fmt.Println(meta.Tenant, err)
fmt.Println(errors.Is(c.Decode("user_metadata", &meta), jwt.ErrNoClaim))
```

Output:

```text
org_7 <nil>
true
```

<a id="Claims.String"></a>

#### func (Claims) String

```go
func (c Claims) String(name string) string
```

String returns a string claim, or "" when the claim is missing or isn't a string.

*Since `v0.3.0 (unreleased)`*

**Example**

```go
c := verified(map[string]any{"sub": "usr_1", "org_id": "org_7"})
fmt.Printf("%q %q\n", c.String("org_id"), c.String("missing"))
```

Output:

```text
"org_7" ""
```

<a id="Claims.Strings"></a>

#### func (Claims) Strings

```go
func (c Claims) Strings(name string) []string
```

Strings returns a claim holding a list of strings, such as "permissions", or one space-separated string, such as "scope". It returns nil when the claim is missing or is neither.

*Since `v0.3.0 (unreleased)`*

**Example**

```go
c := verified(map[string]any{"sub": "usr_1", "scope": "openid read:books", "permissions": []string{"books.book.read"}})
fmt.Println(c.Strings("scope"), c.Strings("permissions"))
```

Output:

```text
[openid read:books] [books.book.read]
```

<a id="Config"></a>
<a id="Config.Issuer"></a>
<a id="Config.Audiences"></a>
<a id="Config.AudienceClaim"></a>
<a id="Config.JWKSURL"></a>
<a id="Config.Algorithms"></a>
<a id="Config.ClockSkew"></a>
<a id="Config.PermissionsClaim"></a>
<a id="Config.ActorFrom"></a>

### type Config

```go
type Config struct {
	// Issuer is the provider's issuer, compared exactly with the "iss"
	// claim, such as "https://example.eu.auth0.com/" (with its trailing
	// slash) or "https://securetoken.google.com/<project>". Required.
	Issuer string
	// Audiences identify this API. A token is accepted when its audience
	// claim contains at least one of them, so tokens the same provider
	// issues for other apps are refused. Required.
	Audiences []string
	// AudienceClaim is the claim holding the audience. Default: "aud".
	// Cognito access tokens carry "client_id" instead.
	AudienceClaim string
	// JWKSURL is where the provider publishes its public keys: https, or
	// http on a loopback address for a provider running locally. Required
	// unless every algorithm is HS256, HS384 or HS512.
	JWKSURL string
	// Algorithms are the accepted "alg" values: RS256, RS384, RS512, PS256,
	// PS384, PS512, ES256, ES384, ES512 or EdDSA, and HS256, HS384 or HS512
	// only with [WithHMACSecret]. "none" is never accepted. Default:
	// [DefaultAlgorithms].
	Algorithms []string
	// ClockSkew is how far the "exp", "nbf" and "iat" claims may be from
	// this server's clock, at most [MaxClockSkew]. Default:
	// [DefaultClockSkew].
	ClockSkew time.Duration
	// PermissionsClaim names the claim holding the caller's permissions: a
	// list of strings, or one space-separated string such as "scope".
	// Default: [DefaultPermissionsClaim]. The default actor mapping drops
	// permissions under [ReservedPermissionPrefix], the operations console's
	// namespace; grant those with ActorFrom instead.
	PermissionsClaim string
	// ActorFrom returns the actor for verified claims, replacing the
	// default: a user whose ID is "sub" and whose permissions are
	// PermissionsClaim, less those under [ReservedPermissionPrefix]. Return
	// a service actor for machine tokens, map the provider's permission
	// names to the app's, or set OrgID. An error refuses the token. The
	// actor's kind must be user or service, with an ID.
	//
	// What it returns is trusted as it stands: the permissions it sets are
	// what guard.Permission and the operations console check, and its StepUp
	// is empty unless it fills it, so a permission an app grants only to a
	// session with a second factor is granted outright. Map the provider's
	// names to the app's rather than passing a claim through.
	ActorFrom func(Claims) (actor.Actor, error)
}
```

Config configures [New](#New).

*Since `v0.3.0 (unreleased)`*

**Example**

```go
idp := newProvider()
defer idp.srv.Close()
auth, err := jwt.New(context.Background(), jwt.Config{
	Issuer:    "https://idp.example.com/",
	Audiences: []string{"https://api.example.com"},
	JWKSURL:   idp.jwksURL(),
	ClockSkew: time.Minute,
	// Machine-to-machine tokens become service actors, and the
	// provider's "read:books" scope becomes the app's permission.
	ActorFrom: func(c jwt.Claims) (actor.Actor, error) {
		kind := actor.KindUser
		if c.String("gty") == "client-credentials" {
			kind = actor.KindService
		}
		a := actor.Actor{Kind: kind, ID: c.Subject}
		for _, scope := range c.Strings("scope") {
			if scope == "read:books" {
				a.Permissions = append(a.Permissions, "books.book.read")
			}
		}
		return a, nil
	},
})
if err != nil {
	panic(err)
}
handler := auth.Middleware(nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
	a, _ := actor.From(r.Context())
	fmt.Println(a.Kind, a.ID, a.Permissions)
}))
req := httptest.NewRequest(http.MethodGet, "/", nil)
req.Header.Set("Authorization", "Bearer "+idp.token(map[string]any{"sub": "client_42@clients", "gty": "client-credentials", "scope": "read:books"}))
handler.ServeHTTP(httptest.NewRecorder(), req)
```

Output:

```text
service client_42@clients [books.book.read]
```

<a id="Option"></a>

### type Option

```go
type Option func(*options)
```

An Option configures [New](#New).

*Since `v0.3.0 (unreleased)`*

**Example**

```go
idp := newProvider()
defer idp.srv.Close()
_, err := jwt.New(context.Background(),
	jwt.Config{Issuer: "https://idp.example.com/", Audiences: []string{"https://api.example.com"}, JWKSURL: idp.jwksURL()},
	jwt.WithHTTPClient(&http.Client{Timeout: 5 * time.Second}),
	jwt.WithClock(time.Now),
)
fmt.Println(err)
```

Output:

```text
<nil>
```

<a id="WithClock"></a>

#### func WithClock

```go
func WithClock(now func() time.Time) Option
```

WithClock sets the clock used to check token times and cache ages, for tests. Default: [time.Now](https://pkg.go.dev/time#Now).

*Since `v0.3.0 (unreleased)`*

**Example**

```go
idp := newProvider()
defer idp.srv.Close()
tomorrow := func() time.Time { return time.Now().Add(24 * time.Hour) }
auth, err := jwt.New(context.Background(),
	jwt.Config{Issuer: "https://idp.example.com/", Audiences: []string{"https://api.example.com"}, JWKSURL: idp.jwksURL()},
	jwt.WithClock(tomorrow))
if err != nil {
	panic(err)
}
_, err = auth.Verify(context.Background(), idp.token(map[string]any{"sub": "usr_1"}))
fmt.Println(err)
```

Output:

```text
jwt: invalid token: expired
```

<a id="WithHMACSecret"></a>

#### func WithHMACSecret

```go
func WithHMACSecret(secret []byte) Option
```

WithHMACSecret sets the shared secret for HS256, HS384 and HS512 tokens, such as a Supabase project's legacy JWT secret. It must be at least 32 bytes, and those algorithms must be listed in [Config.Algorithms](#Config.Algorithms). Prefer a provider's asymmetric keys: anyone holding the secret can issue tokens.

*Since `v0.3.0 (unreleased)`*

**Example**

```go
secret := []byte("a-32-byte-or-longer-project-secret") // from the environment, never in code
auth, err := jwt.New(context.Background(),
	jwt.Config{Issuer: "https://project.supabase.co/auth/v1", Audiences: []string{"authenticated"}, Algorithms: []string{"HS256"}},
	jwt.WithHMACSecret(secret))
if err != nil {
	panic(err)
}
signer, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.HS256, Key: secret}, nil)
token, _ := josejwt.Signed(signer).Claims(map[string]any{
	"iss": "https://project.supabase.co/auth/v1", "aud": "authenticated", "sub": "usr_1", "exp": time.Now().Add(time.Hour).Unix(),
}).Serialize()
c, err := auth.Verify(context.Background(), token)
fmt.Println(c.Subject, err)
```

Output:

```text
usr_1 <nil>
```

<a id="WithHTTPClient"></a>

#### func WithHTTPClient

```go
func WithHTTPClient(c *http.Client) Option
```

WithHTTPClient sets the client used to fetch the provider's keys, such as one with a proxy or custom root certificates. Default: a client with a 10-second timeout that follows at most 3 redirects, each to an allowed URL.

*Since `v0.3.0 (unreleased)`*

**Example**

```go
idp := newProvider()
defer idp.srv.Close()
// A client with the proxy, timeout or root certificates the
// deployment needs.
client := &http.Client{Timeout: 3 * time.Second, Transport: http.DefaultTransport}
_, err := jwt.New(context.Background(),
	jwt.Config{Issuer: "https://idp.example.com/", Audiences: []string{"https://api.example.com"}, JWKSURL: idp.jwksURL()},
	jwt.WithHTTPClient(client))
fmt.Println(err)
```

Output:

```text
<nil>
```
