// Package jwt authenticates requests carrying JSON Web Tokens from an
// external identity provider, such as Auth0, Clerk, Supabase, Firebase or
// Amazon Cognito (ADR-0085). It verifies the token's signature with the
// provider's published keys (JWKS), its issuer, audience and validity
// times, and puts the caller in the request context as an actor.
//
//	idp, err := jwt.New(ctx, jwt.Config{
//		Issuer:    "https://example.eu.auth0.com/",
//		Audiences: []string{"https://api.example.com"},
//		JWKSURL:   "https://example.eu.auth0.com/.well-known/jwks.json",
//	})
//	handler := idp.Middleware(logger)(mux)
//
// Keys are fetched when the authenticator is created, cached as long as the
// provider's Cache-Control allows (between 5 minutes and 24 hours), and
// fetched again when a token names a key the cache doesn't have, at most
// once every 30 seconds. Nothing runs in the background.
//
// Stability: experimental until v0.2.0 (ADR-0015, ADR-0085).
package jwt

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/go-jose/go-jose/v4"
	josejwt "github.com/go-jose/go-jose/v4/jwt"

	"gorbital.dev/actor"
	"gorbital.dev/httpx"
)

// Errors returned by [Authenticator.Verify]. Check them with [errors.Is].
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

// Defaults and bounds of [Config].
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

// DefaultAlgorithms are the signature algorithms accepted unless
// [Config.Algorithms] sets others.
var DefaultAlgorithms = []string{"RS256", "ES256", "EdDSA"}

// Config configures [New].
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

// An Option configures [New].
type Option func(*options)

type options struct {
	client     *http.Client
	now        func() time.Time
	hmacSecret []byte
}

// WithHTTPClient sets the client used to fetch the provider's keys, such as
// one with a proxy or custom root certificates. Default: a client with a
// 10-second timeout that follows at most 3 redirects, each to an allowed
// URL.
func WithHTTPClient(c *http.Client) Option {
	return func(o *options) { o.client = c }
}

// WithClock sets the clock used to check token times and cache ages, for
// tests. Default: [time.Now].
func WithClock(now func() time.Time) Option {
	return func(o *options) { o.now = now }
}

// WithHMACSecret sets the shared secret for HS256, HS384 and HS512 tokens,
// such as a Supabase project's legacy JWT secret. It must be at least 32
// bytes, and those algorithms must be listed in [Config.Algorithms].
// Prefer a provider's asymmetric keys: anyone holding the secret can issue
// tokens.
func WithHMACSecret(secret []byte) Option {
	return func(o *options) { o.hmacSecret = append([]byte(nil), secret...) }
}

// An Authenticator verifies tokens from one provider. Create it with
// [New]. It is safe for concurrent use.
type Authenticator struct {
	issuer           string
	audiences        []string
	audienceClaim    string
	algorithms       []jose.SignatureAlgorithm
	skew             time.Duration
	permissionsClaim string
	actorFrom        func(Claims) (actor.Actor, error)
	now              func() time.Time
	hmacSecret       []byte
	keys             *keySet // nil when only HS algorithms are allowed
}

// asymmetric maps each accepted asymmetric algorithm to a check of its key.
var asymmetric = map[jose.SignatureAlgorithm]bool{
	jose.RS256: true, jose.RS384: true, jose.RS512: true,
	jose.PS256: true, jose.PS384: true, jose.PS512: true,
	jose.ES256: true, jose.ES384: true, jose.ES512: true,
	jose.EdDSA: true,
}

var symmetric = map[jose.SignatureAlgorithm]bool{jose.HS256: true, jose.HS384: true, jose.HS512: true}

// New validates cfg and fetches the provider's keys, within ctx. It returns
// an error for a missing issuer or audience, an unknown or disallowed
// algorithm, a JWKS URL that isn't https (or http on a loopback address), a
// clock skew out of bounds, an HMAC secret that is short or not matched by
// an HS algorithm, or keys that can't be fetched or contain no usable
// signing key, so a misconfigured app fails when it starts.
func New(ctx context.Context, cfg Config, opts ...Option) (*Authenticator, error) {
	o := options{now: time.Now}
	for _, opt := range opts {
		opt(&o)
	}
	a := &Authenticator{
		issuer:           cfg.Issuer,
		audiences:        slices.Clone(cfg.Audiences),
		audienceClaim:    cmp(cfg.AudienceClaim, "aud"),
		skew:             cfg.ClockSkew,
		permissionsClaim: cmp(cfg.PermissionsClaim, DefaultPermissionsClaim),
		actorFrom:        cfg.ActorFrom,
		now:              o.now,
		hmacSecret:       o.hmacSecret,
	}
	switch {
	case cfg.Issuer == "":
		return nil, errors.New("jwt: the issuer is required")
	case len(cfg.Audiences) == 0 || slices.Contains(cfg.Audiences, ""):
		return nil, errors.New("jwt: at least one non-empty audience is required")
	case cfg.ClockSkew < 0 || cfg.ClockSkew > MaxClockSkew:
		return nil, fmt.Errorf("jwt: the clock skew must be between 0 and %s", MaxClockSkew)
	}
	if cfg.ClockSkew == 0 {
		a.skew = DefaultClockSkew
	}
	if a.actorFrom == nil {
		a.actorFrom = a.defaultActor
	}

	algs := cfg.Algorithms
	if len(algs) == 0 {
		algs = DefaultAlgorithms
	}
	needKeys, hmacAlg := false, false
	for _, name := range algs {
		alg := jose.SignatureAlgorithm(name)
		switch {
		case asymmetric[alg]:
			needKeys = true
		case symmetric[alg]:
			hmacAlg = true
		default:
			return nil, fmt.Errorf("jwt: algorithm %q is not supported", name)
		}
		if !slices.Contains(a.algorithms, alg) {
			a.algorithms = append(a.algorithms, alg)
		}
	}
	switch {
	case hmacAlg && len(o.hmacSecret) < 32:
		return nil, errors.New("jwt: HS algorithms need WithHMACSecret with at least 32 bytes")
	case !hmacAlg && o.hmacSecret != nil:
		return nil, errors.New("jwt: WithHMACSecret is set but no HS algorithm is allowed")
	case needKeys && cfg.JWKSURL == "":
		return nil, errors.New("jwt: the JWKS URL is required for asymmetric algorithms")
	case !needKeys && cfg.JWKSURL != "":
		return nil, errors.New("jwt: a JWKS URL is set but only HS algorithms are allowed")
	}
	if !needKeys {
		return a, nil
	}

	u, err := url.Parse(cfg.JWKSURL)
	if err != nil || !allowedKeysURL(u) {
		return nil, fmt.Errorf("jwt: the JWKS URL %q must be an https URL, or http on a loopback address", cfg.JWKSURL)
	}
	client := keysClient(o.client)
	a.keys = newKeySet(u.String(), client, o.now)
	if err := a.keys.load(ctx); err != nil {
		return nil, err
	}
	return a, nil
}

func cmp(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// allowedKeysURL reports whether u is https, or http on a loopback host,
// without user information.
func allowedKeysURL(u *url.URL) bool {
	if u.User != nil || u.Host == "" {
		return false
	}
	switch u.Scheme {
	case "https":
		return true
	case "http":
		host := u.Hostname()
		if host == "localhost" {
			return true
		}
		ip := net.ParseIP(host)
		return ip != nil && ip.IsLoopback()
	}
	return false
}

// keysClient returns the client that fetches the provider's keys. An app's
// own client (WithHTTPClient) is copied, so the redirect allowlist and the
// fetch timeout still apply: without them Go's default policy follows up to
// ten redirects to any scheme and host, and a redirect from the provider's
// JWKS endpoint would let another server supply the keys every token is
// verified against.
func keysClient(c *http.Client) *http.Client {
	if c == nil {
		return &http.Client{Timeout: fetchTimeout, CheckRedirect: checkRedirect}
	}
	copied := *c
	if copied.CheckRedirect == nil {
		copied.CheckRedirect = checkRedirect
	}
	if copied.Timeout <= 0 {
		copied.Timeout = fetchTimeout
	}
	return &copied
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 3 {
		return errors.New("jwt: too many redirects fetching the JWKS")
	}
	if !allowedKeysURL(req.URL) {
		return fmt.Errorf("jwt: refusing a JWKS redirect to %s", req.URL.Redacted())
	}
	return nil
}

// Verify checks token and returns its claims. It returns an error wrapping
// [ErrInvalidToken] when the token isn't acceptable, and
// [ErrKeysUnavailable] when its key isn't cached and the provider's keys
// can't be fetched. Use it outside HTTP middleware, such as for a
// WebSocket's first message.
func (a *Authenticator) Verify(ctx context.Context, token string) (Claims, error) {
	if len(token) > MaxTokenBytes {
		return Claims{}, fmt.Errorf("%w: longer than %d bytes", ErrInvalidToken, MaxTokenBytes)
	}
	tok, err := josejwt.ParseSigned(token, a.algorithms)
	if err != nil {
		return Claims{}, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
	if len(tok.Headers) != 1 {
		return Claims{}, fmt.Errorf("%w: expected one signature", ErrInvalidToken)
	}
	header := tok.Headers[0]
	alg := jose.SignatureAlgorithm(header.Algorithm)

	var key any
	if symmetric[alg] {
		key = a.hmacSecret
	} else {
		jwk, err := a.keys.lookup(ctx, header.KeyID, alg)
		if err != nil {
			return Claims{}, err
		}
		key = jwk.Key
	}

	var (
		registered josejwt.Claims
		c          Claims
	)
	if err := tok.Claims(key, &registered, &c.raw); err != nil {
		return Claims{}, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
	c.Issuer, c.Subject, c.ID = registered.Issuer, registered.Subject, registered.ID
	c.Audience = slices.Clone([]string(registered.Audience))
	if registered.Expiry != nil {
		c.ExpiresAt = registered.Expiry.Time()
	}
	if registered.NotBefore != nil {
		c.NotBefore = registered.NotBefore.Time()
	}
	if registered.IssuedAt != nil {
		c.IssuedAt = registered.IssuedAt.Time()
	}
	if err := a.validate(c, registered); err != nil {
		return Claims{}, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
	return c, nil
}

func (a *Authenticator) validate(c Claims, registered josejwt.Claims) error {
	now := a.now()
	switch {
	case c.Issuer != a.issuer:
		return errors.New("wrong issuer")
	case !a.audienceMatches(c):
		return errors.New("wrong audience")
	case c.Subject == "":
		return errors.New("no subject")
	case registered.Expiry == nil:
		return errors.New("no expiry")
	case now.Add(-a.skew).After(c.ExpiresAt):
		return errors.New("expired")
	case registered.NotBefore != nil && now.Add(a.skew).Before(c.NotBefore):
		return errors.New("not valid yet")
	case registered.IssuedAt != nil && now.Add(a.skew).Before(c.IssuedAt):
		return errors.New("issued in the future")
	}
	return nil
}

func (a *Authenticator) audienceMatches(c Claims) bool {
	audience := c.Audience
	if a.audienceClaim != "aud" {
		audience = c.Strings(a.audienceClaim)
	}
	for _, want := range a.audiences {
		if slices.Contains(audience, want) {
			return true
		}
	}
	return false
}

// ReservedPermissionPrefix is the permission namespace of the framework's
// own operations console (gorbital.dev/gorbital/opshttp). The default actor
// mapping drops permissions under it, because /ops authorizes on the actor's
// permissions alone and the claim a provider puts them in is often not fully
// under the operator's control: an OAuth "scope" claim is asked for by the
// client, and several providers map user-editable metadata into it. An app
// that does grant its operators /ops through the provider says so with
// [Config.ActorFrom], which this never touches.
const ReservedPermissionPrefix = "ops."

func (a *Authenticator) defaultActor(c Claims) (actor.Actor, error) {
	permissions := c.Strings(a.permissionsClaim)
	permissions = slices.DeleteFunc(permissions, func(p string) bool {
		return strings.HasPrefix(p, ReservedPermissionPrefix)
	})
	return actor.Actor{Kind: actor.KindUser, ID: c.Subject, Permissions: permissions}, nil
}

// The middleware's refusals.
var (
	errInvalidToken = httpx.NewProblem(http.StatusUnauthorized, "invalid_token", "the access token is invalid or expired; get a new one")
	errUnavailable  = httpx.NewProblem(http.StatusServiceUnavailable, "auth_unavailable", "authentication is temporarily unavailable")
)

// Middleware returns middleware that authenticates requests carrying a JWT
// in an "Authorization: Bearer" header and puts the caller in the context
// as an actor ([actor.With]), with the client's address and user agent
// ([actor.WithClient]) unless earlier middleware set them. Requests without
// a bearer token, with a token that isn't shaped like a JWT (such as an API
// key another authenticator handles), or that already have an actor
// continue unchanged; routes that need a caller answer them 401.
//
// A JWT that fails verification is refused with 401 invalid_token and a
// WWW-Authenticate header (RFC 6750), rather than continuing anonymously,
// so a client learns to get a new token instead of silently losing access.
// When the provider's keys can't be fetched it answers 503
// auth_unavailable. Refusals are logged at debug level; failed key fetches
// at warn level, at most once every 30 seconds.
func (a *Authenticator) Middleware(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	if a.keys != nil {
		a.keys.setLogger(logger)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			if existing, ok := actor.From(ctx); ok && existing.Kind != actor.KindAnonymous {
				next.ServeHTTP(w, r)
				return
			}
			token, ok := bearerToken(r)
			if !ok || strings.Count(token, ".") != 2 {
				next.ServeHTTP(w, r)
				return
			}
			claims, err := a.Verify(ctx, token)
			if err == nil {
				err = a.authenticate(ctx, claims, &r)
			}
			switch {
			case err == nil:
				next.ServeHTTP(w, r)
			case errors.Is(err, ErrKeysUnavailable):
				httpx.WriteProblem(w, r, errUnavailable)
			default:
				logger.DebugContext(ctx, "bearer token refused", "reason", err.Error())
				w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
				httpx.WriteProblem(w, r, errInvalidToken)
			}
		})
	}
}

// authenticate replaces *r with a request carrying the claims' actor.
func (a *Authenticator) authenticate(ctx context.Context, claims Claims, r **http.Request) error {
	act, err := a.actorFrom(claims)
	if err != nil {
		return fmt.Errorf("%w: ActorFrom: %w", ErrInvalidToken, err)
	}
	if (act.Kind != actor.KindUser && act.Kind != actor.KindService) || act.ID == "" {
		return fmt.Errorf("%w: ActorFrom returned a %q actor with ID %q; want a user or service with an ID", ErrInvalidToken, act.Kind, act.ID)
	}
	ctx = actor.With(ctx, act)
	if _, ok := actor.ClientFrom(ctx); !ok {
		host, _, err := net.SplitHostPort((*r).RemoteAddr)
		if err != nil {
			host = (*r).RemoteAddr
		}
		ctx = actor.WithClient(ctx, actor.Client{IP: host, UserAgent: (*r).UserAgent()})
	}
	key := "user_id"
	if act.Kind == actor.KindService {
		key = "service_id"
	}
	httpx.AccessNoteFrom(ctx).Add(slog.String(key, act.ID))
	*r = (*r).WithContext(ctx)
	return nil
}

// bearerToken returns the token of an "Authorization: Bearer" header; the
// scheme is case-insensitive.
func bearerToken(r *http.Request) (string, bool) {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != ""
}
