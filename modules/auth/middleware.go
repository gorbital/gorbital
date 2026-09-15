package auth

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
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

// WithPrincipal returns a copy of ctx carrying p and its actor.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	ctx = context.WithValue(ctx, principalKey{}, p)
	return actor.With(ctx, actor.Actor{Kind: actor.KindUser, ID: p.UserID, Permissions: p.Permissions, StepUp: p.StepUp})
}

// PrincipalFrom returns the authenticated principal in ctx.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

// WithClientInfo returns a copy of ctx carrying c.
func WithClientInfo(ctx context.Context, c ClientInfo) context.Context {
	return context.WithValue(ctx, clientKey{}, c.Clean())
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
	cookie string
	logger *slog.Logger
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

// Middleware stores every request's [ClientInfo] in its context, and
// authenticates requests carrying a session token in an "Authorization:
// Bearer" header or the session cookie, putting the principal and its actor
// in the context. Requests without a valid token continue anonymously: use
// cases decide what needs authentication. When the authenticator fails for
// another reason (the database is down) it responds 503, rather than
// treating a signed-in user as anonymous.
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
			p, err := a.Authenticate(r.Context(), token)
			switch {
			case errors.Is(err, ErrUnauthenticated):
				next.ServeHTTP(w, r)
			case err != nil:
				o.logger.ErrorContext(r.Context(), "authenticate request", "err", err)
				httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusServiceUnavailable, "auth_unavailable", "authentication is temporarily unavailable"))
			default:
				next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), p)))
			}
		})
	}
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
