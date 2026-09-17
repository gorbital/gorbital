package auth

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gorbital.dev/actor"
	"gorbital.dev/httpx"
)

// RecentVerification is how long after verifying a second factor a session
// can change the account's sign-in methods without the password (ADR-0044),
// and how long after signing in an account without a password can confirm
// sensitive changes (ADR-0046).
const RecentVerification = 10 * time.Minute

// Principal is the authenticated user of a request.
type Principal struct {
	UserID    string
	SessionID string
	// Permissions are granted by the user's roles to this session.
	Permissions []string
	// StepUp are permissions of roles requiring two-factor authentication,
	// held back because the session isn't verified with a second factor.
	StepUp []string
	// MFAVerified reports whether the session was verified with a second
	// factor, and MFAVerifiedAt when it last was.
	MFAVerified   bool
	MFAVerifiedAt time.Time
	// SignedInAt is when the session started.
	SignedInAt time.Time

	// APIKeyID is the API key that authenticated the request, empty for a
	// session (ADR-0058). SessionID is empty for an API key.
	APIKeyID string
	// Scopes limit an API key to these permissions; empty means its owner's.
	// See [Principal.Restrict].
	Scopes []string
	// ServiceAccountID is set, instead of UserID, for an API key of a service
	// account: a non-human principal whose actor has kind service.
	ServiceAccountID string
	// OrgID is the organisation an organisation's service account belongs
	// to; it can act in no other.
	OrgID string
}

// RecentlySignedIn reports whether the session started less than
// [RecentVerification] before now.
func (p Principal) RecentlySignedIn(now time.Time) bool {
	return !p.SignedInAt.IsZero() && now.Sub(p.SignedInAt) < RecentVerification
}

// RecentlyVerified reports whether the session verified a second factor
// less than [RecentVerification] before now. A stolen session that verified
// one long ago doesn't pass.
func (p Principal) RecentlyVerified(now time.Time) bool {
	return p.MFAVerified && !p.MFAVerifiedAt.IsZero() && now.Sub(p.MFAVerifiedAt) < RecentVerification
}

// An Authenticator resolves a session token, returning [ErrUnauthenticated]
// for an unknown, ended or expired session. The app's auth use cases
// implement it.
type Authenticator interface {
	Authenticate(ctx context.Context, token string) (Principal, error)
}

type (
	principalKey struct{}
	clientKey    struct{}
)

// WithPrincipal returns a copy of ctx carrying p and its actor: a user, or
// a service for a service account's API key. An API key's actor gets only
// the permissions [Principal.Restrict] leaves it.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	p.Permissions, p.StepUp = p.Restrict(p.Permissions, p.StepUp)
	ctx = context.WithValue(ctx, principalKey{}, p)
	a := actor.Actor{Kind: actor.KindUser, ID: p.UserID, Permissions: p.Permissions, StepUp: p.StepUp}
	if p.ServiceAccountID != "" {
		a.Kind, a.ID = actor.KindService, p.ServiceAccountID
	}
	return actor.With(ctx, a)
}

// PrincipalFrom returns the authenticated principal in ctx.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

// WithClientInfo returns a copy of ctx carrying c. It also sets c as the
// context's [actor.Client], so audit events any module records for the
// request carry the client's IP address and user agent.
func WithClientInfo(ctx context.Context, c ClientInfo) context.Context {
	c = c.Clean()
	ctx = actor.WithClient(ctx, actor.Client{IP: c.IP, UserAgent: c.UserAgent})
	return context.WithValue(ctx, clientKey{}, c)
}

// ClientInfoFromContext returns the client details [Middleware] stored for
// the request, or empty details.
func ClientInfoFromContext(ctx context.Context) ClientInfo {
	c, _ := ctx.Value(clientKey{}).(ClientInfo)
	return c
}

// ClientInfoFrom returns the client's IP address (from RemoteAddr: run
// trusted-proxy middleware first behind a proxy) and user agent.
func ClientInfoFrom(r *http.Request) ClientInfo {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return ClientInfo{IP: host, UserAgent: r.UserAgent()}.Clean()
}

type middlewareOptions struct {
	cookie  string
	logger  *slog.Logger
	apiKeys APIKeyAuthenticator
}

// A MiddlewareOption configures [Middleware].
type MiddlewareOption interface{ apply(*middlewareOptions) }

type middlewareOptionFunc func(*middlewareOptions)

func (f middlewareOptionFunc) apply(o *middlewareOptions) { f(o) }

// WithCookieName sets the session cookie name. Default: [DefaultCookieName].
func WithCookieName(name string) MiddlewareOption {
	return middlewareOptionFunc(func(o *middlewareOptions) { o.cookie = name })
}

// WithLogger sets the logger for authentication failures. Default: discard.
func WithLogger(logger *slog.Logger) MiddlewareOption {
	return middlewareOptionFunc(func(o *middlewareOptions) { o.logger = logger })
}

// WithAPIKeys authenticates bearer tokens that start with [APIKeyPrefix]
// with a (ADR-0058). Without it, such tokens are ignored and the request
// continues anonymously.
func WithAPIKeys(a APIKeyAuthenticator) MiddlewareOption {
	return middlewareOptionFunc(func(o *middlewareOptions) { o.apiKeys = a })
}

// Middleware stores every request's [ClientInfo] in its context, and
// authenticates requests carrying a session token in an "Authorization:
// Bearer" header or the session cookie, putting the principal and its actor
// in the context. Requests without a valid token continue anonymously: use
// cases decide what needs authentication. When the authenticator fails for
// another reason (the database is down) it responds 503, rather than
// treating a signed-in user as anonymous.
//
// A token starting with [APIKeyPrefix] is never passed to the session
// authenticator: in the Authorization header it goes to the API key
// authenticator of [WithAPIKeys], and in a cookie it is ignored. An API key
// authenticator's [*RateLimitError] responds 429 too_many_attempts.
//
// Cookie-authenticated requests need cross-origin protection
// (httpx.CrossOrigin) earlier in the chain.
func Middleware(a Authenticator, opts ...MiddlewareOption) func(http.Handler) http.Handler {
	o := middlewareOptions{cookie: DefaultCookieName, logger: slog.New(slog.DiscardHandler)}
	for _, opt := range opts {
		opt.apply(&o)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r = r.WithContext(WithClientInfo(r.Context(), ClientInfoFrom(r)))
			token := TokenFrom(r, o.cookie)
			if token == "" {
				next.ServeHTTP(w, r)
				return
			}
			var (
				p   Principal
				err error
			)
			switch {
			case !IsAPIKey(token):
				p, err = a.Authenticate(r.Context(), token)
			case o.apiKeys == nil || !bearer(r):
				err = ErrUnauthenticated
			default:
				p, err = o.apiKeys.AuthenticateAPIKey(r.Context(), token)
			}
			var limited *RateLimitError
			switch {
			case errors.Is(err, ErrUnauthenticated):
				next.ServeHTTP(w, r)
			case errors.As(err, &limited):
				w.Header().Set("Retry-After", strconv.Itoa(max(int(limited.RetryAfter.Round(time.Second).Seconds()), 1)))
				httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusTooManyRequests, "too_many_attempts", "too many failed authentications from this network; try again later"))
			case err != nil:
				o.logger.ErrorContext(r.Context(), "authenticate request", "err", err)
				httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusServiceUnavailable, "auth_unavailable", "authentication is temporarily unavailable"))
			default:
				// The request's log record names the user (httpx.AccessLog).
				httpx.AccessNoteFrom(r.Context()).Add(slog.String("user_id", p.UserID))
				next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), p)))
			}
		})
	}
}

// bearer reports whether the request's token came from the Authorization
// header rather than a cookie.
func bearer(r *http.Request) bool {
	_, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok
}

// TokenFrom returns the request's bearer token, or else its session cookie.
func TokenFrom(r *http.Request, cookieName string) string {
	if token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		return strings.TrimSpace(token)
	}
	if c, err := r.Cookie(cookieName); err == nil {
		return c.Value
	}
	return ""
}

// SessionCookie returns the cookie that carries token for browsers: Secure,
// HttpOnly, SameSite=Lax, path "/", expiring at expires.
func SessionCookie(name, token string, expires time.Time) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    token,
		Path:     "/",
		Expires:  expires.UTC(),
		MaxAge:   max(int(time.Until(expires).Seconds()), 1),
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
}

// ClearSessionCookie returns a cookie that removes the session cookie.
func ClearSessionCookie(name string) *http.Cookie {
	return &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}
}
