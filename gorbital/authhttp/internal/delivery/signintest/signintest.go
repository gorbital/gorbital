// Package signintest checks an app's sign-in configuration from the Dev
// Portal (ADR-0087): offline checks of each method's variables, network
// checks against Google, Apple and GitHub, and live tests that go through the
// real provider, a passkey ceremony, an authenticator app code or an email,
// without creating accounts, identity links, sessions, audit events or
// database rows. Everything a test holds lives in this process's memory,
// expires within minutes and is used once.
//
// A Tester exists only while the development console does (APP_ENV
// development with DEV_CONSOLE_TOKEN): its endpoints are served under
// /_dev/auth/test/ behind the console's checks, and the only entry points
// outside it are the provider callbacks, which recognise a test's state
// before sign-in looks at it, and the passkey ceremony page, which does
// nothing without a ticket from the console.
package signintest

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"gorbital.dev/gorbital"
	authlib "gorbital.dev/modules/auth"
	"gorbital.dev/modules/auth/passkey"
	"gorbital.dev/modules/auth/social"
)

// Limits of what a Tester keeps in memory.
const (
	// SocialTTL is how long a started Google, Apple or GitHub test waits for
	// the provider, as a sign-in does.
	SocialTTL = 10 * time.Minute
	// PasskeyTTL is how long a passkey ceremony test can take.
	PasskeyTTL = passkey.CeremonyTTL
	// TOTPTTL is how long an authenticator app test's secret is kept.
	TOTPTTL = 10 * time.Minute
	// ResultTTL is how long a finished result can be read.
	ResultTTL = 30 * time.Minute
	// MaxPending is how many tests of each kind can wait at once.
	MaxPending = 16
	// MaxResults is how many results are kept; the oldest go first.
	MaxResults = 64
	// MaxTOTPAttempts is how many codes one authenticator app test checks.
	MaxTOTPAttempts = 5
)

// Check statuses.
const (
	StatusOK   = "ok"
	StatusWarn = "warn"
	StatusFail = "fail"
	StatusSkip = "skip"
)

// Console pages a check's fix points at.
const (
	LinkEnvironment = "environment"
	LinkTunnel      = "tunnel"
	LinkMail        = "mail"
	LinkGuide       = "guide"
)

// Result states.
const (
	StatePending = "pending"
	StatePassed  = "passed"
	StateFailed  = "failed"
	StateExpired = "expired"
)

// Method keys.
const (
	MethodGoogle           = social.Google
	MethodApple            = social.Apple
	MethodGitHub           = social.GitHub
	MethodPasskeys         = "passkeys"
	MethodAuthenticatorApp = "authenticator_app"
	MethodEmail            = "email"
)

// Errors of the endpoints.
var (
	// ErrNotConfigured reports a method this app doesn't configure.
	ErrNotConfigured = errors.New("this sign-in method isn't configured")
	// ErrInvalidResultURL reports a result_url that isn't a page on this
	// machine.
	ErrInvalidResultURL = errors.New("result_url must be an http URL on localhost, 127.0.0.1 or [::1] (the Dev Portal)")
	// ErrTooManyTests reports too many tests waiting at once.
	ErrTooManyTests = errors.New("too many tests are waiting; finish or wait for one")
	// ErrTestNotFound reports an unknown, used or expired test.
	ErrTestNotFound = errors.New("no such test: it finished, expired or never existed")
)

// UnavailableError reports a live test that can't run with this
// configuration, and why.
type UnavailableError struct {
	Reason string
	Link   string
}

func (e *UnavailableError) Error() string { return e.Reason }

// Check is one finding about a method's configuration.
type Check struct {
	// Code is stable: the UI and the docs key on it.
	Code   string `json:"code"`
	Status string `json:"status"`
	// Message says what was found; Fix what to do about it.
	Message   string   `json:"message"`
	Fix       string   `json:"fix,omitempty"`
	Variables []string `json:"variables,omitempty"`
	Link      string   `json:"link,omitempty"`
}

// Live describes a method's live test.
type Live struct {
	// Kind is redirect (Google, Apple, GitHub), ceremony (passkeys), code
	// (authenticator apps) or email.
	Kind      string `json:"kind"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
	Link      string `json:"link,omitempty"`
}

// Method is a sign-in method with its offline checks.
type Method struct {
	Key         string  `json:"key"`
	Name        string  `json:"name"`
	Configured  bool    `json:"configured"`
	Checks      []Check `json:"checks"`
	Live        Live    `json:"live"`
	CallbackURL string  `json:"callback_url,omitempty"`
	IDToken     bool    `json:"id_token,omitempty"`
	Origin      string  `json:"origin,omitempty"`
	RPID        string  `json:"rp_id,omitempty"`
}

// Overview is GET /_dev/auth/test.
type Overview struct {
	PublicURL string   `json:"public_url"`
	Methods   []Method `json:"methods"`
}

// LiveChecks is POST /_dev/auth/test/{provider}/check.
type LiveChecks struct {
	Method string  `json:"method"`
	Checks []Check `json:"checks"`
}

// Start is a started live test: open URL in a browser.
type Start struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Identity is what a provider said about the person, never tokens.
type Identity struct {
	Subject       string `json:"subject"`
	Email         string `json:"email,omitempty"`
	EmailVerified bool   `json:"email_verified"`
	PrivateEmail  bool   `json:"private_email,omitempty"`
	Name          string `json:"name,omitempty"`
	Audience      string `json:"audience,omitempty"`
	HostedDomain  string `json:"hosted_domain,omitempty"`
}

// PasskeyInfo describes the throwaway passkey a ceremony test created.
type PasskeyInfo struct {
	RPID           string `json:"rp_id"`
	Origin         string `json:"origin"`
	CredentialID   string `json:"credential_id"`
	BackupEligible bool   `json:"backup_eligible"`
	BackupState    bool   `json:"backup_state"`
	UserVerified   bool   `json:"user_verified"`
}

// Result is a live test's outcome.
type Result struct {
	ID     string `json:"id"`
	Method string `json:"method"`
	// Kind is redirect, ceremony or id_token.
	Kind       string       `json:"kind"`
	State      string       `json:"state"`
	Code       string       `json:"code,omitempty"`
	Message    string       `json:"message,omitempty"`
	Fix        string       `json:"fix,omitempty"`
	Link       string       `json:"link,omitempty"`
	Identity   *Identity    `json:"identity,omitempty"`
	Passkey    *PasskeyInfo `json:"passkey,omitempty"`
	Warnings   []Check      `json:"warnings,omitempty"`
	StartedAt  time.Time    `json:"started_at"`
	FinishedAt *time.Time   `json:"finished_at,omitempty"`
	ExpiresAt  time.Time    `json:"expires_at"`
}

// Config is what a Tester tests.
type Config struct {
	// AppName names the app in authenticator apps and passkey prompts.
	AppName string
	// App is the app's configuration.
	App gorbital.Config
	// Google, Apple and GitHub are the providers as sign-in builds them,
	// nil when off.
	Google, Apple, GitHub *social.Provider
	// Endpoints are the providers' endpoints (zero: the real ones).
	GoogleEndpoints, AppleEndpoints, GitHubEndpoints social.Endpoints
	// Passkeys is the relying party, nil when off.
	Passkeys *passkey.Service
	// Keyring is AUTH_ENCRYPTION_KEYS, nil when unset.
	Keyring *authlib.Keyring
	// HTTPClient makes the network checks' requests (default: a client
	// with a 10 second timeout).
	HTTPClient *http.Client
	Now        func() time.Time
	Logger     *slog.Logger
}

// Tester runs sign-in tests. It is safe for concurrent use.
type Tester struct {
	cfg Config

	mu       sync.Mutex
	social   map[[sha256.Size]byte]*socialTest // by the state's hash
	passkeys map[[sha256.Size]byte]*passkeyTest
	totps    map[string]*totpTest
	results  map[string]*Result
	order    []string // result IDs, oldest first
}

// New returns a Tester for cfg.
func New(cfg Config) *Tester {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}
	if cfg.GoogleEndpoints.TokenURL == "" {
		cfg.GoogleEndpoints = social.GoogleEndpoints()
	}
	if cfg.AppleEndpoints.TokenURL == "" {
		cfg.AppleEndpoints = social.AppleEndpoints()
	}
	if cfg.GitHubEndpoints.TokenURL == "" {
		cfg.GitHubEndpoints = social.GitHubEndpoints()
	}
	return &Tester{
		cfg: cfg, social: map[[sha256.Size]byte]*socialTest{}, passkeys: map[[sha256.Size]byte]*passkeyTest{},
		totps: map[string]*totpTest{}, results: map[string]*Result{},
	}
}

// Result returns the result of the test id.
func (t *Tester) Result(id string) (Result, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sweep()
	r, ok := t.results[id]
	if !ok {
		return Result{}, ErrTestNotFound
	}
	return *r, nil
}

// addResult keeps r, dropping the oldest results over MaxResults. The caller
// holds t.mu.
func (t *Tester) addResult(r *Result) {
	t.results[r.ID] = r
	t.order = append(t.order, r.ID)
	for len(t.order) > MaxResults {
		delete(t.results, t.order[0])
		t.order = t.order[1:]
	}
}

// finish records a result's outcome. The caller holds t.mu.
func (t *Tester) finish(r *Result, state, code, message, fix, link string) {
	now := t.cfg.Now()
	r.State, r.Code, r.Message, r.Fix, r.Link, r.FinishedAt = state, code, message, fix, link, &now
}

// sweep forgets what expired. The caller holds t.mu.
func (t *Tester) sweep() {
	now := t.cfg.Now()
	for h, s := range t.social {
		if !now.Before(s.expiresAt) {
			if r := t.results[s.id]; r != nil && r.State == StatePending {
				t.finish(r, StateExpired, "expired", "the provider didn't return within "+SocialTTL.String(), "start the test again and finish it in the provider's window", "")
			}
		}
		// A test's state stays known as long as its result, so a late or
		// replayed return is still recognised as a test's.
		if now.Sub(s.expiresAt) >= ResultTTL {
			delete(t.social, h)
		}
	}
	for h, p := range t.passkeys {
		if !now.Before(p.expiresAt) {
			delete(t.passkeys, h)
			if r := t.results[p.id]; r != nil && r.State == StatePending {
				t.finish(r, StateExpired, "expired", "the passkey ceremony didn't finish within "+PasskeyTTL.String(), "start the test again", "")
			}
		}
	}
	for id, s := range t.totps {
		if !now.Before(s.expiresAt) {
			delete(t.totps, id)
		}
	}
	for len(t.order) > 0 {
		r := t.results[t.order[0]]
		if r != nil && now.Sub(r.StartedAt) < ResultTTL {
			break
		}
		delete(t.results, t.order[0])
		t.order = t.order[1:]
	}
}

// newSecret returns 256 random bits, base64url, and their hash.
func newSecret() (string, [sha256.Size]byte) {
	b := make([]byte, 32)
	_, _ = rand.Read(b) // never fails (crypto/rand)
	s := base64.RawURLEncoding.EncodeToString(b)
	return s, sha256.Sum256([]byte(s))
}

// CheckResultURL returns raw when it is where a finished test may send the
// browser: an http or https URL on localhost, 127.0.0.1 or [::1] with no
// user information or fragment, such as the Dev Portal's result page. It
// returns ErrInvalidResultURL.
func CheckResultURL(raw string) (string, error) {
	if raw == "" || len(raw) > 512 {
		return "", ErrInvalidResultURL
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" || u.Opaque != "" || strings.ContainsAny(raw, "\\\r\n\t ") {
		return "", ErrInvalidResultURL
	}
	switch strings.ToLower(u.Hostname()) {
	case "localhost", "127.0.0.1", "::1":
	default:
		return "", ErrInvalidResultURL
	}
	if u.Port() == "" && strings.HasSuffix(u.Host, ":") {
		return "", ErrInvalidResultURL
	}
	return u.String(), nil
}

// resultRedirect is where the browser goes after test id of method.
func resultRedirect(resultURL, id, method string) string {
	return resultURL + "#" + url.Values{"id": {id}, "method": {method}}.Encode()
}

var (
	// jwtLike matches compact JWS/JWE values and other long base64url runs,
	// which never belong in a message.
	jwtLike   = regexp.MustCompile(`[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}(\.[A-Za-z0-9_-]+){0,2}`)
	longToken = regexp.MustCompile(`[A-Za-z0-9_\-+/=]{41,}`)
)

// redact removes what could be a token or secret from a provider's message,
// and bounds its length.
func redact(msg string, secrets ...string) string {
	msg = strings.Map(func(r rune) rune {
		if r < ' ' && r != '\n' || r == 0x7f {
			return -1
		}
		return r
	}, msg)
	for _, s := range secrets {
		if len(s) >= 6 {
			msg = strings.ReplaceAll(msg, s, "[redacted]")
		}
	}
	msg = jwtLike.ReplaceAllString(msg, "[redacted]")
	msg = longToken.ReplaceAllString(msg, "[redacted]")
	if len(msg) > 500 {
		n := 500
		for n > 0 && !utf8.RuneStart(msg[n]) {
			n--
		}
		msg = msg[:n] + "…"
	}
	return msg
}
