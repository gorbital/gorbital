package signintest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	authlib "gorbital.dev/modules/auth"
	"gorbital.dev/modules/auth/social"
)

// socialTest is a started Google, Apple or GitHub round trip. Only the
// state's hash is the map key; the nonce and PKCE verifier stay here.
type socialTest struct {
	id        string
	provider  string
	nonce     string
	verifier  string
	resultURL string
	expiresAt time.Time
	// used is set once the provider returned: the state stays known until
	// the result is forgotten, so a replayed or late return still lands on
	// the result page and never reaches sign-in.
	used bool
}

// provider returns the configured provider named name, or nil.
func (t *Tester) provider(name string) *social.Provider {
	switch name {
	case social.Google:
		return t.cfg.Google
	case social.Apple:
		return t.cfg.Apple
	case social.GitHub:
		return t.cfg.GitHub
	}
	return nil
}

// CallbackURL is the app's callback for provider, as sign-in sends it.
func (t *Tester) CallbackURL(provider string) string {
	return t.cfg.App.Auth.PublicURL + "/v1/auth/" + provider + "/callback"
}

// socialUnavailable returns why provider's web round trip can't run, or nil.
func (t *Tester) socialUnavailable(provider string) *UnavailableError {
	p := t.provider(provider)
	switch {
	case p == nil:
		return &UnavailableError{Reason: providerLabel(provider) + " sign-in isn't configured", Link: LinkEnvironment}
	case !p.Web() && provider == social.Apple:
		return &UnavailableError{Reason: "Apple's web sign-in needs APPLE_SERVICES_ID; with only APPLE_BUNDLE_IDS, verify an ID token from your iOS app instead", Link: LinkEnvironment}
	case !p.Web():
		return &UnavailableError{Reason: providerLabel(provider) + "'s web sign-in isn't configured", Link: LinkEnvironment}
	}
	if provider == social.Apple {
		if u, err := url.Parse(t.cfg.App.Auth.PublicURL); err != nil || u.Scheme != "https" || isLocalHost(u.Hostname()) {
			return &UnavailableError{
				Reason: "Apple accepts only https return URLs on a real domain, and APP_PUBLIC_URL is " + t.cfg.App.Auth.PublicURL +
					": start a named tunnel and apply its .env changes, then test again",
				Link: LinkTunnel,
			}
		}
	}
	return nil
}

// StartSocial starts a test round trip through provider's sign-in page back
// to the app's real callback URL, which then sends the browser to
// resultURL. It returns ErrNotConfigured, an *UnavailableError,
// ErrInvalidResultURL or ErrTooManyTests.
func (t *Tester) StartSocial(provider, resultURL string) (Start, error) {
	if t.provider(provider) == nil {
		return Start{}, ErrNotConfigured
	}
	if u := t.socialUnavailable(provider); u != nil {
		return Start{}, u
	}
	resultURL, err := CheckResultURL(resultURL)
	if err != nil {
		return Start{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sweep()
	if t.pendingSocial() >= MaxPending || len(t.social) >= MaxResults {
		return Start{}, ErrTooManyTests
	}
	now := t.cfg.Now()
	state, hash := newSecret()
	nonce, _ := newSecret()
	st := &socialTest{
		id: authlib.NewID("slt"), provider: provider, nonce: nonce, verifier: social.NewPKCEVerifier(),
		resultURL: resultURL, expiresAt: now.Add(SocialTTL),
	}
	t.social[hash] = st
	t.addResult(&Result{ID: st.id, Method: provider, Kind: "redirect", State: StatePending, StartedAt: now, ExpiresAt: st.expiresAt})
	return Start{ID: st.id, URL: t.provider(provider).AuthCodeURL(t.CallbackURL(provider), state, nonce, st.verifier), ExpiresAt: st.expiresAt}, nil
}

// pendingSocial counts the round trips waiting for the provider. The
// caller holds t.mu.
func (t *Tester) pendingSocial() int {
	n := 0
	now := t.cfg.Now()
	for _, st := range t.social {
		if !st.used && now.Before(st.expiresAt) {
			n++
		}
	}
	return n
}

// maxCallbackBody bounds what the interceptor reads of Apple's form post.
const maxCallbackBody = 64 << 10

// callbackParams returns the state, code, error and Apple user values of a
// provider's return, reading Apple's form post and restoring the body for
// sign-in when the request isn't a test's.
func callbackParams(r *http.Request) (state, code, providerError, user string) {
	if r.Method == http.MethodGet {
		q := r.URL.Query()
		return q.Get("state"), q.Get("code"), q.Get("error"), ""
	}
	if r.Body == nil {
		return "", "", "", ""
	}
	read, err := io.ReadAll(io.LimitReader(r.Body, maxCallbackBody+1))
	r.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(read), r.Body), r.Body}
	if err != nil || len(read) > maxCallbackBody {
		return "", "", "", ""
	}
	form, err := url.ParseQuery(string(read))
	if err != nil {
		return "", "", "", ""
	}
	return form.Get("state"), form.Get("code"), form.Get("error"), form.Get("user")
}

// callbackPaths are the provider returns the interceptor looks at.
var callbackPaths = map[string]string{
	http.MethodGet + " /v1/auth/google/callback": social.Google,
	http.MethodGet + " /v1/auth/github/callback": social.GitHub,
	http.MethodPost + " /v1/auth/apple/callback": social.Apple,
}

// Callbacks returns middleware for sign-in's routes that finishes test
// round trips: a provider's return whose state is a test's is answered here,
// with a redirect to the test's result page, and never reaches sign-in. Any
// other request passes on unchanged (Apple's form body is read and put
// back).
func (t *Tester) Callbacks(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provider, ok := callbackPaths[r.Method+" "+r.URL.Path]
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		state, code, providerError, user := callbackParams(r)
		location, ok := t.FinishSocial(r.Context(), provider, state, code, providerError, user)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		h := w.Header()
		h.Set("Cache-Control", "no-store")
		h.Set("Location", location)
		w.WriteHeader(http.StatusSeeOther)
	})
}

// FinishSocial finishes the test whose state provider returned, when state
// is a test's: it exchanges the code and verifies the identity as sign-in
// does, records the result and returns where to send the browser. It
// reports false for any other state, which is sign-in's to handle. The
// state is used up whatever the outcome.
func (t *Tester) FinishSocial(ctx context.Context, provider, state, code, providerError, appleUser string) (string, bool) {
	if state == "" || len(state) > 256 {
		return "", false
	}
	hash := sha256.Sum256([]byte(state))
	t.mu.Lock()
	st, ok := t.social[hash]
	var replay, expired bool
	if ok {
		replay, expired = st.used, !t.cfg.Now().Before(st.expiresAt)
		st.used = true
	}
	t.mu.Unlock()
	if !ok {
		return "", false
	}
	redirect := resultRedirect(st.resultURL, st.id, st.provider)
	if replay {
		return redirect, true // the result stays as the first return left it
	}

	outcome := func(state, code, message, fix, link string, identity *Identity, warnings []Check) (string, bool) {
		t.mu.Lock()
		defer t.mu.Unlock()
		if r := t.results[st.id]; r != nil {
			t.finish(r, state, code, message, fix, link)
			r.Identity, r.Warnings = identity, warnings
		}
		t.cfg.Logger.InfoContext(ctx, "sign-in test finished", "method", st.provider, "state", state, "code", code)
		return redirect, true
	}
	switch {
	case expired:
		return outcome(StateExpired, "expired", "the provider returned after the test expired ("+SocialTTL.String()+")", "start the test again", "", nil, nil)
	case provider != st.provider:
		return outcome(StateFailed, "invalid_request", "the test started with "+providerLabel(st.provider)+" but returned to "+providerLabel(provider)+"'s callback",
			"check the redirect URI registered with "+providerLabel(st.provider)+": "+t.CallbackURL(st.provider), LinkGuide, nil, nil)
	case providerError != "":
		c := sanitizeCode(providerError)
		f := describeFailure(st.provider, c, "", t.CallbackURL(st.provider))
		return outcome(StateFailed, f.code, f.message, f.fix, f.link, nil, nil)
	case code == "":
		return outcome(StateFailed, "invalid_request", providerLabel(provider)+" returned without an authorization code", "start the test again", "", nil, nil)
	}
	p := t.provider(st.provider)
	if p == nil {
		return outcome(StateFailed, "not_configured", providerLabel(provider)+" sign-in is no longer configured", "", LinkEnvironment, nil, nil)
	}
	tok, err := p.Exchange(ctx, t.CallbackURL(st.provider), code, st.verifier, st.nonce)
	if err != nil {
		f := t.classify(st.provider, err, code)
		return outcome(StateFailed, f.code, f.message, f.fix, f.link, nil, nil)
	}
	id := identityOf(tok.Identity)
	if id.Name == "" {
		id.Name = appleName(appleUser)
	}
	return outcome(StatePassed, "ok", providerLabel(st.provider)+" sign-in works: the code was exchanged with the client secret and the identity verified as sign-in does",
		"", "", id, identityWarnings(tok.Identity))
}

// VerifyIDToken checks an ID token a native app got from Google's or
// Apple's SDK against nonce as sign-in's token endpoints do (for Apple, the
// token carries the nonce's SHA-256 in hex), and records the result. It
// returns ErrNotConfigured, or ErrTestNotFound's sibling errors for input
// sign-in would refuse before verifying.
func (t *Tester) VerifyIDToken(ctx context.Context, provider, idToken, nonce string) (Result, error) {
	p := t.provider(provider)
	if p == nil || provider == social.GitHub {
		return Result{}, ErrNotConfigured
	}
	if idToken == "" || len(idToken) > 16_384 || nonce == "" || len(nonce) > 256 {
		return Result{}, ErrInvalidRequest
	}
	now := t.cfg.Now()
	r := &Result{ID: authlib.NewID("slt"), Method: provider, Kind: "id_token", State: StatePending, StartedAt: now, ExpiresAt: now.Add(ResultTTL)}
	expected := nonce
	if provider == social.Apple {
		sum := sha256.Sum256([]byte(nonce))
		expected = hex.EncodeToString(sum[:])
	}
	id, err := p.VerifyIDToken(ctx, idToken, expected)
	var f failure
	if err != nil {
		f = t.classify(provider, err, idToken, nonce)
		if f.code == "nonce_mismatch" && provider == social.Apple {
			if _, rawErr := p.VerifyIDToken(ctx, idToken, nonce); rawErr == nil {
				f.message = "the ID token carries the nonce itself, not its SHA-256"
				f.fix = "in the iOS app, put the SHA-256 of the nonce in hex in ASAuthorizationAppleIDRequest.nonce, and send the nonce itself to POST /v1/auth/apple/token"
			}
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sweep()
	t.addResult(r)
	if err != nil {
		t.finish(r, StateFailed, f.code, f.message, f.fix, f.link)
	} else {
		t.finish(r, StatePassed, "ok", "the ID token verifies: signature, issuer, audience "+id.Audience+", age and nonce, as sign-in checks them", "", "")
		r.Identity, r.Warnings = identityOf(id), identityWarnings(id)
	}
	t.cfg.Logger.InfoContext(ctx, "sign-in test finished", "method", provider, "state", r.State, "code", r.Code)
	return *r, nil
}

// ErrInvalidRequest reports input a test can't use.
var ErrInvalidRequest = errors.New("id_token (at most 16384 characters) and nonce (at most 256) are required")

func identityOf(id social.Identity) *Identity {
	return &Identity{
		Subject: id.Subject, Email: id.Email, EmailVerified: id.EmailVerified, PrivateEmail: id.PrivateEmail,
		Name: id.Name, Audience: id.Audience, HostedDomain: id.HostedDomain,
	}
}

// identityWarnings are what would stop this identity creating an account.
func identityWarnings(id social.Identity) []Check {
	switch {
	case id.Email == "":
		return []Check{{Code: "email_missing", Status: StatusWarn, Message: providerLabel(id.Provider) + " returned no email address, so sign-in can't create an account for this person",
			Fix: emailMissingFix(id.Provider)}}
	case !id.EmailVerified:
		return []Check{{Code: "email_not_verified", Status: StatusWarn, Message: providerLabel(id.Provider) + " hasn't verified " + id.Email + ", so sign-in refuses a new account for it (social_email_unverified)",
			Fix: "verify the address with the provider, or test with an account whose address is verified"}}
	}
	return nil
}

func emailMissingFix(provider string) string {
	if provider == social.GitHub {
		return "the GitHub account needs a verified primary email address that isn't a users.noreply.github.com one, and the OAuth app the user:email scope (gorbital asks for it)"
	}
	return "the email scope must be granted; for Apple, the email is sent only the first time a person signs in to the Services ID"
}

// appleName reads the name Apple posts once, as
// {"name":{"firstName":…,"lastName":…}}.
func appleName(user string) string {
	var u struct {
		Name struct {
			FirstName string `json:"firstName"`
			LastName  string `json:"lastName"`
		} `json:"name"`
	}
	if user == "" || len(user) > 4096 || json.Unmarshal([]byte(user), &u) != nil {
		return ""
	}
	return strings.TrimSpace(u.Name.FirstName + " " + u.Name.LastName)
}

// sanitizeCode keeps a provider's error parameter to a short code.
func sanitizeCode(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if b.Len() >= 64 {
			break
		}
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "provider_error"
	}
	return b.String()
}

func providerLabel(provider string) string {
	switch provider {
	case social.Google:
		return "Google"
	case social.Apple:
		return "Apple"
	case social.GitHub:
		return "GitHub"
	}
	return provider
}

func isLocalHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host == "localhost" || strings.HasSuffix(host, ".localhost") || host == "127.0.0.1" || host == "::1" || strings.HasPrefix(host, "127.")
}
