package usecase_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"slices"
	"sync"
	"testing"
	"time"

	authlib "gorbital.dev/modules/auth"
	"gorbital.dev/modules/auth/social"
	"gorbital.dev/modules/auth/social/socialtest"

	authdomain "example.com/plateful/internal/modules/auth/domain"
	authusecase "example.com/plateful/internal/modules/auth/usecase"
)

const returnTo = "https://app.example.com/welcome"

type socialFixture struct {
	*fixture
	srv *socialtest.Server
}

// newSocialFixture configures Google, Apple and GitHub against a fake
// provider.
func newSocialFixture(t *testing.T, configure ...func(*authusecase.Config)) *socialFixture {
	t.Helper()
	srv := socialtest.New(t)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	google, err := social.NewGoogle(social.GoogleConfig{
		ClientID: "web-client", ClientSecret: "secret", NativeClientIDs: []string{"ios-client"}, Endpoints: srv.Endpoints(),
	})
	if err != nil {
		t.Fatal(err)
	}
	apple, err := social.NewApple(social.AppleConfig{
		TeamID: "TEAM123456", KeyID: "KEY1234567", PrivateKey: key, ServicesID: "com.example.web", BundleIDs: []string{"com.example.app"},
		Endpoints: srv.Endpoints(),
	})
	if err != nil {
		t.Fatal(err)
	}
	gitHub, err := social.NewGitHub(social.GitHubConfig{ClientID: "github-client", ClientSecret: "github-secret", Endpoints: srv.GitHubEndpoints()})
	if err != nil {
		t.Fatal(err)
	}
	withSocial := func(c *authusecase.Config) {
		c.Google, c.Apple, c.GitHub = google, apple, gitHub
		c.PublicURL, c.DefaultReturnTo = "http://localhost:8080", "http://localhost:8080/docs"
		c.ReturnOrigins = []string{"http://localhost:8080", "https://app.example.com"}
	}
	return &socialFixture{fixture: newFixture(t, append([]func(*authusecase.Config){withKeys, withSocial}, configure...)...), srv: srv}
}

// webSignIn starts a web sign-in with provider and finishes it as the
// provider would for claims, returning refresh as the refresh token.
func (f *socialFixture) webSignIn(t *testing.T, provider string, c socialtest.Claims, refresh string) (authusecase.SocialResult, error) {
	t.Helper()
	start, err := f.svc.StartSocialSignIn(requestCtx(), provider, returnTo)
	if err != nil {
		t.Fatalf("StartSocialSignIn(%s) error = %v", provider, err)
	}
	q := query(t, start.URL)
	c.Nonce, c.Audience = q.Get("nonce"), q.Get("client_id")
	return f.svc.FinishSocialSignIn(requestCtx(), provider, q.Get("state"), start.BrowserToken, f.srv.Code(c, refresh), "")
}

// nativeSignIn signs in with an ID token for claims, as an iOS app would.
func (f *socialFixture) nativeSignIn(t *testing.T, provider string, c socialtest.Claims, code, name string) (authusecase.LoginResult, error) {
	t.Helper()
	nonce, _, err := f.svc.SocialNonce(requestCtx(), provider)
	if err != nil {
		t.Fatalf("SocialNonce(%s) error = %v", provider, err)
	}
	c.Nonce = nonce
	if provider == social.Apple {
		sum := sha256.Sum256([]byte(nonce))
		c.Nonce = hex.EncodeToString(sum[:])
	}
	return f.svc.SignInWithIDToken(requestCtx(), provider, f.srv.IDToken(c), nonce, code, name)
}

// linkIdentity links the identity of an ID token for claims to the
// signed-in user of ctx, as a signed-in app would.
func (f *socialFixture) linkIdentity(t *testing.T, ctx context.Context, provider string, c socialtest.Claims, password string) (authdomain.Identity, bool, error) {
	t.Helper()
	nonce, _, err := f.svc.SocialNonce(requestCtx(), provider)
	if err != nil {
		t.Fatalf("SocialNonce(%s) error = %v", provider, err)
	}
	c.Nonce = nonce
	if c.Audience == "" {
		c.Audience = map[string]string{social.Google: "web-client", social.Apple: "com.example.web"}[provider]
	}
	if provider == social.Apple {
		sum := sha256.Sum256([]byte(nonce))
		c.Nonce = hex.EncodeToString(sum[:])
	}
	return f.svc.LinkIdentity(ctx, provider, f.srv.IDToken(c), nonce, "", "", password)
}

func query(t *testing.T, raw string) url.Values {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query()
}

var ada = socialtest.Claims{Subject: "g-ada", Email: "Ada@Example.com", EmailVerified: true, Name: "Ada"}

func TestSocialWebSignIn(t *testing.T) {
	f := newSocialFixture(t)

	start, err := f.svc.StartSocialSignIn(requestCtx(), social.Google, "")
	if err != nil || query(t, start.URL).Get("redirect_uri") != "http://localhost:8080/v1/auth/google/callback" || start.BrowserToken == "" {
		t.Fatalf("StartSocialSignIn() = %+v, %v", start, err)
	}

	// A new person gets an account without a password. Google doesn't manage
	// example.com, so the address isn't verified by it.
	res, err := f.webSignIn(t, social.Google, ada, "")
	if err != nil || res.Token == "" || res.ReturnTo != returnTo || res.User.EmailVerified() || res.User.HasPassword() || res.User.Email != "Ada@Example.com" {
		t.Fatalf("FinishSocialSignIn(new person) = %+v, %v", res, err)
	}
	if !hasAll(f.audit.actions(), "auth.user.registered", "auth.identity.linked", "auth.login.succeeded") {
		t.Errorf("audit actions = %v", f.audit.actions())
	}

	// The same person signs in to the same account.
	again, err := f.webSignIn(t, social.Google, ada, "")
	if err != nil || again.User.ID != res.User.ID {
		t.Fatalf("FinishSocialSignIn(returning person) = %+v, %v", again, err)
	}

	// Apple with the same verified email doesn't link by itself: Apple isn't
	// authoritative for example.com. Signed in, the user links it.
	withApple := socialtest.Claims{Subject: "001.ada", Email: "ada@example.com", Extra: map[string]any{"email_verified": "true"}}
	if _, err := f.webSignIn(t, social.Apple, withApple, "refresh-web"); !errors.Is(err, authdomain.ErrSocialLinkRequired) || f.emails.count("sign_in_method_added") != 0 {
		t.Fatalf("FinishSocialSignIn(Apple, same email) error = %v, %d emails; want ErrSocialLinkRequired", err, f.emails.count("sign_in_method_added"))
	}
	if e, _ := f.audit.find("auth.login.failed"); e.Metadata["reason"] != "social_link_required" || e.ResourceID != res.User.ID {
		t.Errorf("refused link audit event = %+v", e)
	}
	if _, added, err := f.linkIdentity(t, f.principalCtx(t, res.Token), social.Apple, withApple, ""); err != nil || !added || f.emails.count("sign_in_method_added") != 1 {
		t.Fatalf("LinkIdentity(Apple) = %t, %v; %d emails", added, err, f.emails.count("sign_in_method_added"))
	}
	if linked, err := f.webSignIn(t, social.Apple, withApple, ""); err != nil || linked.User.ID != res.User.ID {
		t.Fatalf("FinishSocialSignIn(linked Apple) = %+v, %v", linked, err)
	}
	identities, err := f.svc.ListIdentities(f.principalCtx(t, res.Token))
	g := slices.IndexFunc(identities, func(i authdomain.Identity) bool { return i.Provider == social.Google })
	if err != nil || len(identities) != 2 || g < 0 || identities[g].Name != "Ada" || identities[g].LastUsedAt == nil {
		t.Errorf("ListIdentities() = %+v, %v", identities, err)
	}

	// A verified password account keeps its password when linked, here by
	// its Google Workspace domain.
	f.signUp(t, "bob@example.com")
	bob, err := f.webSignIn(t, social.Google, socialtest.Claims{Subject: "g-bob", Email: "bob@example.com", EmailVerified: true, Extra: map[string]any{"hd": "example.com"}}, "")
	if err != nil || !bob.User.HasPassword() {
		t.Fatalf("FinishSocialSignIn(verified password account) = %+v, %v", bob, err)
	}
	if _, err := f.svc.Login(requestCtx(), "bob@example.com", password); err != nil {
		t.Errorf("Login(bob) after linking error = %v", err)
	}
}

func TestSocialLinkRemovesUnverifiedPassword(t *testing.T) {
	f := newSocialFixture(t)
	// Someone registers eve's address without owning it.
	if err := f.svc.Register(requestCtx(), "eve@example.com", password); err != nil {
		t.Fatal(err)
	}
	eve := socialtest.Claims{Subject: "g-eve", Email: "eve@example.com", EmailVerified: true}
	if _, err := f.webSignIn(t, social.Google, eve, ""); !errors.Is(err, authdomain.ErrSocialLinkRequired) {
		t.Fatalf("FinishSocialSignIn(unverified account, address Google doesn't manage) error = %v, want ErrSocialLinkRequired", err)
	}
	eve.Extra = map[string]any{"hd": "example.com"}
	res, err := f.webSignIn(t, social.Google, eve, "")
	if err != nil || res.User.HasPassword() || !res.User.EmailVerified() {
		t.Fatalf("FinishSocialSignIn(unverified account) = %+v, %v", res, err)
	}
	if _, err := f.svc.Login(requestCtx(), "eve@example.com", password); !errors.Is(err, authdomain.ErrInvalidCredentials) {
		t.Errorf("Login(the registrant's password) error = %v, want ErrInvalidCredentials", err)
	}
}

func TestSocialWebSignInRejections(t *testing.T) {
	f := newSocialFixture(t)
	ctx := requestCtx()

	for _, bad := range []string{"https://evil.example/x", "javascript:alert(1)", "/relative", "https://user@app.example.com/"} {
		if _, err := f.svc.StartSocialSignIn(ctx, social.Google, bad); !errors.Is(err, authdomain.ErrInvalidReturnTo) {
			t.Errorf("StartSocialSignIn(return_to %q) error = %v, want ErrInvalidReturnTo", bad, err)
		}
	}
	if _, err := newFixture(t).svc.StartSocialSignIn(ctx, social.Google, ""); !errors.Is(err, authdomain.ErrSocialUnavailable) {
		t.Errorf("StartSocialSignIn() without providers error = %v", err)
	}

	finish := func(provider, browser string, c socialtest.Claims, prepare func(*authusecase.SocialStart, url.Values)) (authusecase.SocialResult, error) {
		start, err := f.svc.StartSocialSignIn(ctx, social.Google, returnTo)
		if err != nil {
			t.Fatal(err)
		}
		q := query(t, start.URL)
		if prepare != nil {
			prepare(&start, q)
		}
		if browser == "" {
			browser = start.BrowserToken
		}
		if c.Nonce == "" {
			c.Nonce = q.Get("nonce")
		}
		c.Audience = "web-client"
		return f.svc.FinishSocialSignIn(ctx, provider, q.Get("state"), browser, f.srv.Code(c, ""), "")
	}

	if res, err := finish(social.Google, "another-browser", ada, nil); !errors.Is(err, authdomain.ErrInvalidState) || res.ReturnTo != returnTo {
		t.Errorf("FinishSocialSignIn(other browser) = %q, %v; want ErrInvalidState returning to the frontend", res.ReturnTo, err)
	}
	if _, err := finish(social.Apple, "", ada, nil); !errors.Is(err, authdomain.ErrInvalidState) {
		t.Errorf("FinishSocialSignIn(other provider) error = %v", err)
	}
	if _, err := finish(social.Google, "", ada, func(*authusecase.SocialStart, url.Values) { f.clock.advance(authdomain.OAuthStateTTL) }); !errors.Is(err, authdomain.ErrInvalidState) {
		t.Errorf("FinishSocialSignIn(expired) error = %v", err)
	}
	if _, err := finish(social.Google, "", socialtest.Claims{Subject: "g-x", Email: "x@example.com", EmailVerified: true, Nonce: "not-the-nonce"}, nil); !errors.Is(err, authdomain.ErrInvalidSocialToken) {
		t.Errorf("FinishSocialSignIn(other nonce) error = %v", err)
	}
	if _, err := finish(social.Google, "", socialtest.Claims{Subject: "g-x", Email: "x@example.com"}, nil); !errors.Is(err, authdomain.ErrSocialEmailUnverified) {
		t.Errorf("FinishSocialSignIn(unverified email) error = %v", err)
	}

	// A state works once, even after a success.
	start, _ := f.svc.StartSocialSignIn(ctx, social.Google, returnTo)
	q := query(t, start.URL)
	c := ada
	c.Nonce, c.Audience = q.Get("nonce"), "web-client"
	if _, err := f.svc.FinishSocialSignIn(ctx, social.Google, q.Get("state"), start.BrowserToken, f.srv.Code(c, ""), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.FinishSocialSignIn(ctx, social.Google, q.Get("state"), start.BrowserToken, f.srv.Code(c, ""), ""); !errors.Is(err, authdomain.ErrInvalidState) {
		t.Errorf("FinishSocialSignIn(used state) error = %v", err)
	}
	// The person cancelling at the provider sends no code.
	start, _ = f.svc.StartSocialSignIn(ctx, social.Google, returnTo)
	if _, err := f.svc.FinishSocialSignIn(ctx, social.Google, query(t, start.URL).Get("state"), start.BrowserToken, "", ""); !errors.Is(err, authdomain.ErrInvalidSocialToken) {
		t.Errorf("FinishSocialSignIn(no code) error = %v", err)
	}
}

func TestSocialSignInAsksForSecondFactor(t *testing.T) {
	f := newSocialFixture(t)
	res, err := f.webSignIn(t, social.Google, ada, "")
	if err != nil {
		t.Fatal(err)
	}
	// An account without a password sets up 2FA after a recent sign-in.
	f.enroll(t, f.principalCtx(t, res.Token))

	again, err := f.webSignIn(t, social.Google, ada, "")
	if err != nil || again.Token != "" || again.Challenge == nil || !slices.Equal(again.Challenge.Methods, []string{"totp", "recovery_code"}) {
		t.Fatalf("FinishSocialSignIn() with 2FA = %+v, %v; want a challenge", again, err)
	}
}

func TestNativeSocialSignIn(t *testing.T) {
	f := newSocialFixture(t)
	c := socialtest.Claims{Subject: "g-native", Audience: "ios-client", Email: "ada@example.com", EmailVerified: true}
	if res, err := f.nativeSignIn(t, social.Google, c, "", ""); err != nil || res.Token == "" {
		t.Fatalf("SignInWithIDToken(Google iOS) = %+v, %v", res, err)
	}

	// A nonce works once, and the audience must be one of the app's clients.
	nonce, _, _ := f.svc.SocialNonce(requestCtx(), social.Google)
	c.Nonce = nonce
	token := f.srv.IDToken(c)
	if _, err := f.svc.SignInWithIDToken(requestCtx(), social.Google, token, nonce, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.SignInWithIDToken(requestCtx(), social.Google, token, nonce, "", ""); !errors.Is(err, authdomain.ErrInvalidSocialToken) {
		t.Errorf("SignInWithIDToken(used nonce) error = %v", err)
	}
	other := c
	other.Audience = "someone-else"
	if _, err := f.nativeSignIn(t, social.Google, other, "", ""); !errors.Is(err, authdomain.ErrInvalidSocialToken) {
		t.Errorf("SignInWithIDToken(other audience) error = %v", err)
	}

	// Apple: the token carries the nonce's SHA-256, the name comes from the
	// app, and the code gives a refresh token.
	apple := socialtest.Claims{
		Subject: "001.native", Audience: "com.example.app", Email: "x1@privaterelay.appleid.com",
		Extra: map[string]any{"email_verified": "true", "is_private_email": "true"},
	}
	code := f.srv.Code(socialtest.Claims{Subject: "001.native", Audience: "com.example.app"}, "refresh-native")
	res, err := f.nativeSignIn(t, social.Apple, apple, code, "Ada Lovelace")
	if err != nil {
		t.Fatalf("SignInWithIDToken(Apple) error = %v", err)
	}
	identities, _ := f.svc.ListIdentities(f.principalCtx(t, res.Token))
	if len(identities) != 1 || identities[0].Name != "Ada Lovelace" || !identities[0].PrivateEmail {
		t.Errorf("ListIdentities() after Apple = %+v", identities)
	}
	nonce, _, _ = f.svc.SocialNonce(requestCtx(), social.Apple)
	apple.Nonce = nonce // not hashed
	if _, err := f.svc.SignInWithIDToken(requestCtx(), social.Apple, f.srv.IDToken(apple), nonce, "", ""); !errors.Is(err, authdomain.ErrInvalidSocialToken) {
		t.Errorf("SignInWithIDToken(Apple, unhashed nonce) error = %v", err)
	}
}

func TestIdentitiesRemovalAndAccountDeletion(t *testing.T) {
	f := newSocialFixture(t)
	apple := socialtest.Claims{Subject: "001.ada", Audience: "com.example.app", Email: "ada@icloud.com", Extra: map[string]any{"email_verified": "true"}}
	res, err := f.nativeSignIn(t, social.Apple, apple, f.srv.Code(socialtest.Claims{Subject: "001.ada", Audience: "com.example.app"}, "refresh-1"), "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := f.principalCtx(t, res.Token)
	identities, _ := f.svc.ListIdentities(ctx)

	// The only way to sign in can't be removed.
	if err := f.svc.RemoveIdentity(ctx, identities[0].ID, ""); !errors.Is(err, authdomain.ErrLastSignInMethod) {
		t.Errorf("RemoveIdentity(last method) error = %v", err)
	}
	if err := f.svc.RemoveIdentity(ctx, "idn_unknown", ""); !errors.Is(err, authdomain.ErrIdentityNotFound) {
		t.Errorf("RemoveIdentity(unknown) error = %v", err)
	}

	// With Google linked, Google can go; an old session needs a new sign-in.
	// Google doesn't manage iCloud addresses, so the signed-in user links it.
	google := socialtest.Claims{Subject: "g-ada", Email: "ada@icloud.com", EmailVerified: true}
	if _, _, err := f.linkIdentity(t, ctx, social.Google, google, ""); err != nil {
		t.Fatal(err)
	}
	identities, _ = f.svc.ListIdentities(ctx)
	googleID := identities[slices.IndexFunc(identities, func(i authdomain.Identity) bool { return i.Provider == social.Google })].ID
	f.clock.advance(authlib.RecentVerification)
	stale := f.principalCtx(t, res.Token)
	if err := f.svc.RemoveIdentity(stale, googleID, ""); !errors.Is(err, authdomain.ErrInvalidCredentials) {
		t.Errorf("RemoveIdentity(old sign-in) error = %v, want ErrInvalidCredentials", err)
	}
	fresh, err := f.webSignIn(t, social.Google, google, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx = f.principalCtx(t, fresh.Token)
	if err := f.svc.RemoveIdentity(ctx, googleID, ""); err != nil || f.emails.count("sign_in_method_removed") != 1 || len(f.srv.Revoked()) != 0 {
		t.Errorf("RemoveIdentity(Google) = %v, %d emails, revoked %v", err, f.emails.count("sign_in_method_removed"), f.srv.Revoked())
	}

	// An account without a password is deleted after a recent sign-in, and
	// Apple's token is revoked.
	if err := f.svc.DeleteAccount(stale, "", authdomain.SecondFactor{}); !errors.Is(err, authdomain.ErrInvalidCredentials) {
		t.Errorf("DeleteAccount(old sign-in) error = %v", err)
	}
	if err := f.svc.DeleteAccount(ctx, "", authdomain.SecondFactor{}); err != nil {
		t.Fatalf("DeleteAccount(recent sign-in) error = %v", err)
	}
	if revoked := f.srv.Revoked(); len(revoked) != 0 {
		t.Errorf("revoked tokens during deletion = %v, want none: the job revokes them", revoked)
	}
	if got, err := f.svc.RevokeProviderTokens(context.Background()); err != nil || got.Revoked != 1 {
		t.Fatalf("RevokeProviderTokens() = %+v, %v", got, err)
	}
	if revoked := f.srv.Revoked(); len(revoked) != 1 || revoked[0] != "refresh-1" {
		t.Errorf("revoked tokens after the job = %v, want refresh-1", revoked)
	}

	// Signing in again creates a new account.
	again, err := f.webSignIn(t, social.Google, google, "")
	if err != nil || again.User.ID == res.User.ID {
		t.Errorf("sign-in after deletion = %+v, %v; want a new account", again.User, err)
	}
}

// TestProviderTokenRevocationRetries checks that a provider failing to
// revoke a queued token is retried with backoff, and abandoned with an audit
// event after MaxRevocationAttempts.
func TestProviderTokenRevocationRetries(t *testing.T) {
	f := newSocialFixture(t)
	ctx := context.Background()
	deleteAppleAccount := func(subject, refresh string) {
		t.Helper()
		claims := socialtest.Claims{Subject: subject, Audience: "com.example.app", Email: subject + "@example.com", Extra: map[string]any{"email_verified": "true"}}
		res, err := f.nativeSignIn(t, social.Apple, claims, f.srv.Code(socialtest.Claims{Subject: subject, Audience: "com.example.app"}, refresh), "")
		if err != nil {
			t.Fatal(err)
		}
		if err := f.svc.DeleteAccount(f.principalCtx(t, res.Token), "", authdomain.SecondFactor{}); err != nil {
			t.Fatal(err)
		}
	}

	deleteAppleAccount("001.retry", "refresh-retry")
	f.srv.FailRevocations(true)
	if got, err := f.svc.RevokeProviderTokens(ctx); err != nil || got != (authdomain.RevocationResult{Retrying: 1}) {
		t.Fatalf("RevokeProviderTokens() with the provider down = %+v, %v; want one retrying", got, err)
	}
	if got, err := f.svc.RevokeProviderTokens(ctx); err != nil || got != (authdomain.RevocationResult{}) {
		t.Errorf("RevokeProviderTokens() before the backoff = %+v, %v; want nothing due", got, err)
	}
	f.clock.advance(authdomain.RevocationBackoff(1))
	f.srv.FailRevocations(false)
	if got, err := f.svc.RevokeProviderTokens(ctx); err != nil || got.Revoked != 1 || !slices.Equal(f.srv.Revoked(), []string{"refresh-retry"}) {
		t.Fatalf("RevokeProviderTokens() after the backoff = %+v, %v; revoked %v", got, err, f.srv.Revoked())
	}

	deleteAppleAccount("001.gone", "refresh-gone")
	f.srv.FailRevocations(true)
	var total authdomain.RevocationResult
	for range authdomain.MaxRevocationAttempts {
		got, err := f.svc.RevokeProviderTokens(ctx)
		if err != nil {
			t.Fatal(err)
		}
		total.Retrying, total.Abandoned = total.Retrying+got.Retrying, total.Abandoned+got.Abandoned
		f.clock.advance(6*time.Hour + time.Minute)
	}
	if total != (authdomain.RevocationResult{Retrying: authdomain.MaxRevocationAttempts - 1, Abandoned: 1}) {
		t.Errorf("results over %d attempts = %+v", authdomain.MaxRevocationAttempts, total)
	}
	if _, ok := f.audit.find("auth.identity.revocation_abandoned"); !ok {
		t.Error("no auth.identity.revocation_abandoned event")
	}
}

func TestRevocationBackoff(t *testing.T) {
	for attempts, want := range map[int]time.Duration{0: time.Minute, 1: time.Minute, 2: 2 * time.Minute, 5: 16 * time.Minute, 9: 256 * time.Minute, 10: 6 * time.Hour, 50: 6 * time.Hour} {
		if got := authdomain.RevocationBackoff(attempts); got != want {
			t.Errorf("RevocationBackoff(%d) = %s, want %s", attempts, got, want)
		}
	}
}

// TestConcurrentFirstSignIn signs one new person in from several requests
// at once: every request gets the same account.
func TestConcurrentFirstSignIn(t *testing.T) {
	f := newSocialFixture(t)
	const n = 6
	tokens, nonces := make([]string, n), make([]string, n)
	for i := range n {
		nonce, _, err := f.svc.SocialNonce(requestCtx(), social.Google)
		if err != nil {
			t.Fatal(err)
		}
		nonces[i] = nonce
		tokens[i] = f.srv.IDToken(socialtest.Claims{Subject: "g-race", Audience: "ios-client", Email: "race@example.com", EmailVerified: true, Nonce: nonce})
	}
	users, errs := make([]string, n), make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			res, err := f.svc.SignInWithIDToken(requestCtx(), social.Google, tokens[i], nonces[i], "", "")
			users[i], errs[i] = res.User.ID, err
		})
	}
	wg.Wait()
	for i := range n {
		if errs[i] != nil || users[i] != users[0] {
			t.Errorf("sign-in %d = %q, %v; want account %q", i, users[i], errs[i], users[0])
		}
	}
}

func TestAppleNotifications(t *testing.T) {
	f := newSocialFixture(t)
	apple := socialtest.Claims{Subject: "001.ada", Audience: "com.example.app", Email: "ada@icloud.com", Extra: map[string]any{"email_verified": "true"}}
	res, err := f.nativeSignIn(t, social.Apple, apple, "", "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if err := f.svc.HandleAppleNotification(ctx, f.srv.Notification("com.example.app", social.NotificationEmailDisabled, "001.ada")); err != nil {
		t.Errorf("HandleAppleNotification(email-disabled) error = %v", err)
	}
	if err := f.svc.HandleAppleNotification(ctx, f.srv.Notification("com.example.app", social.NotificationConsentRevoked, "001.unknown")); err != nil {
		t.Errorf("HandleAppleNotification(unknown person) error = %v", err)
	}
	if err := f.svc.HandleAppleNotification(ctx, "garbage"); !errors.Is(err, authdomain.ErrInvalidSocialToken) {
		t.Errorf("HandleAppleNotification(garbage) error = %v", err)
	}

	// Revoking consent unlinks Apple and, with no other way in, signs out.
	if err := f.svc.HandleAppleNotification(ctx, f.srv.Notification("com.example.app", social.NotificationConsentRevoked, "001.ada")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Authenticate(ctx, res.Token); !errors.Is(err, authlib.ErrUnauthenticated) {
		t.Errorf("Authenticate() after consent revoked error = %v, want ErrUnauthenticated", err)
	}
	if again, err := f.nativeSignIn(t, social.Apple, apple, "", ""); err != nil || again.User.ID != res.User.ID {
		t.Errorf("sign-in after consent revoked = %+v, %v; want the same account linked again", again.User, err)
	}
}

// TestAppleNotificationReplay: a leaked notification can't be replayed, now
// or after the person signs in with Apple again (security review AUTH-M-3).
func TestAppleNotificationReplay(t *testing.T) {
	f := newSocialFixture(t)
	// Apple signs notifications with the real clock; link at the same time.
	f.clock.advance(time.Until(f.clock.now()) * -1)
	ctx := context.Background()
	apple := socialtest.Claims{Subject: "001.bob", Audience: "com.example.app", Email: "bob@icloud.com", Extra: map[string]any{"email_verified": "true"}}
	if _, err := f.nativeSignIn(t, social.Apple, apple, "", ""); err != nil {
		t.Fatal(err)
	}
	revoked := f.srv.Notification("com.example.app", social.NotificationConsentRevoked, "001.bob")
	if err := f.svc.HandleAppleNotification(ctx, revoked); err != nil {
		t.Fatal(err)
	}

	// Signed in with Apple again, a replay of the same payload changes nothing.
	f.clock.advance(time.Minute)
	res, err := f.nativeSignIn(t, social.Apple, apple, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.HandleAppleNotification(ctx, revoked); err != nil {
		t.Fatalf("HandleAppleNotification(replay) error = %v", err)
	}
	if _, err := f.svc.Authenticate(ctx, res.Token); err != nil {
		t.Errorf("Authenticate() after a replayed notification error = %v, want the session kept", err)
	}
	if e, _ := f.audit.find("auth.identity.apple_notification"); e.Metadata["replayed"] != true {
		t.Errorf("replay audit event = %+v", e)
	}

	// A new notification about an event before the identity was linked
	// leaves it linked.
	f.srv.Now = func() time.Time { return time.Now().Add(-10 * time.Minute) }
	early := f.srv.Notification("com.example.app", social.NotificationAccountDelete, "001.bob")
	f.srv.Now = time.Now
	if err := f.svc.HandleAppleNotification(ctx, early); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Authenticate(ctx, res.Token); err != nil {
		t.Errorf("Authenticate() after an event from before the link error = %v, want the session kept", err)
	}

	// A year-old notification is refused.
	f.srv.Now = func() time.Time { return time.Now().AddDate(-1, 0, 0) }
	if err := f.svc.HandleAppleNotification(ctx, f.srv.Notification("com.example.app", social.NotificationConsentRevoked, "001.bob")); !errors.Is(err, authdomain.ErrInvalidSocialToken) {
		t.Errorf("HandleAppleNotification(a year old) error = %v, want ErrInvalidSocialToken", err)
	}
}

// TestSocialLinksOnlyAuthoritativeEmails: a provider links an existing
// account by itself only when it manages the address, since a verified email
// at any other domain may have changed hands; otherwise the owner links it
// while signed in (security review AUTH-M-1).
func TestSocialLinksOnlyAuthoritativeEmails(t *testing.T) {
	f := newSocialFixture(t, func(c *authusecase.Config) { c.Passkeys = testPasskeys })
	for _, email := range []string{"ada@gmail.com", "bob@corp.example", "cy@icloud.com", "dee@example.com"} {
		f.signUp(t, email)
	}
	for name, tt := range map[string]struct {
		provider string
		claims   socialtest.Claims
		link     bool
	}{
		"Gmail":                     {social.Google, socialtest.Claims{Subject: "g-ada", Email: "ada@gmail.com", EmailVerified: true}, true},
		"Google Workspace":          {social.Google, socialtest.Claims{Subject: "g-bob", Email: "bob@corp.example", EmailVerified: true, Extra: map[string]any{"hd": "corp.example"}}, true},
		"iCloud":                    {social.Apple, socialtest.Claims{Subject: "001.cy", Email: "cy@icloud.com", Extra: map[string]any{"email_verified": "true"}}, true},
		"personal Google elsewhere": {social.Google, socialtest.Claims{Subject: "g-dee", Email: "dee@example.com", EmailVerified: true}, false},
		"other Workspace domain":    {social.Google, socialtest.Claims{Subject: "g-dee2", Email: "dee@example.com", EmailVerified: true, Extra: map[string]any{"hd": "corp.example"}}, false},
		"Apple, other domain":       {social.Apple, socialtest.Claims{Subject: "001.dee", Email: "dee@example.com", Extra: map[string]any{"email_verified": "true"}}, false},
	} {
		res, err := f.webSignIn(t, tt.provider, tt.claims, "")
		switch {
		case tt.link && (err != nil || res.Token == ""):
			t.Errorf("%s: FinishSocialSignIn() = %+v, %v; want linked and signed in", name, res, err)
		case !tt.link && (!errors.Is(err, authdomain.ErrSocialLinkRequired) || res.Token != ""):
			t.Errorf("%s: FinishSocialSignIn() error = %v; want ErrSocialLinkRequired", name, err)
		}
	}

	// Signed in, the owner links the provider after the password.
	ctx, _ := f.login(t, "dee@example.com")
	dee := socialtest.Claims{Subject: "g-dee", Email: "dee@example.com", EmailVerified: true}
	if _, _, err := f.linkIdentity(t, ctx, social.Google, dee, "wrong password here"); !errors.Is(err, authdomain.ErrInvalidCredentials) {
		t.Errorf("LinkIdentity(wrong password) error = %v, want ErrInvalidCredentials", err)
	}
	if e, _ := f.audit.find("auth.reauth.failed"); e.Metadata["reason"] != "invalid_credentials" {
		t.Errorf("reauth audit event = %+v", e)
	}
	identity, added, err := f.linkIdentity(t, ctx, social.Google, dee, password)
	if err != nil || !added || identity.Provider != social.Google || identity.Email != "dee@example.com" {
		t.Fatalf("LinkIdentity() = %+v, %t, %v", identity, added, err)
	}
	if _, added, err := f.linkIdentity(t, ctx, social.Google, dee, password); err != nil || added {
		t.Errorf("LinkIdentity(already linked) = %t, %v; want nothing added", added, err)
	}
	if res, err := f.webSignIn(t, social.Google, dee, ""); err != nil || res.Token == "" {
		t.Errorf("FinishSocialSignIn(linked identity) = %+v, %v", res, err)
	}
	// Another account's identity can't be linked, and a token doesn't work
	// without its nonce.
	if _, _, err := f.linkIdentity(t, ctx, social.Google, socialtest.Claims{Subject: "g-ada", Email: "ada@gmail.com", EmailVerified: true}, password); !errors.Is(err, authdomain.ErrIdentityInUse) {
		t.Errorf("LinkIdentity(another account's identity) error = %v, want ErrIdentityInUse", err)
	}
	token := f.srv.IDToken(socialtest.Claims{Subject: "g-dee3", Audience: "web-client", Nonce: "made-up"})
	if _, _, err := f.svc.LinkIdentity(ctx, social.Google, token, "made-up", "", "", password); !errors.Is(err, authdomain.ErrInvalidSocialToken) {
		t.Errorf("LinkIdentity(unknown nonce) error = %v, want ErrInvalidSocialToken", err)
	}
	if _, _, err := f.svc.LinkIdentity(requestCtx(), social.Google, token, "made-up", "", "", password); !errors.Is(err, authlib.ErrUnauthenticated) {
		t.Errorf("LinkIdentity(signed out) error = %v, want ErrUnauthenticated", err)
	}
}

// TestNonAuthoritativeSocialAccountIsClaimedByEmail: a Google account for an
// address Google doesn't manage creates an unverified account, so the
// address's owner proving it by email removes that identity (security review
// AUTH-M-1, AUTH-S-1). A provider that manages the address verifies it.
func TestNonAuthoritativeSocialAccountIsClaimedByEmail(t *testing.T) {
	f := newSocialFixture(t)
	attacker := socialtest.Claims{Subject: "g-attacker", Email: "victim@corp.example", EmailVerified: true}
	res, err := f.webSignIn(t, social.Google, attacker, "")
	if err != nil || res.Token == "" || res.User.EmailVerified() {
		t.Fatalf("FinishSocialSignIn(address Google doesn't manage) = %+v, %v; want a session on an unverified account", res.User, err)
	}
	if gmail, err := f.webSignIn(t, social.Google, socialtest.Claims{Subject: "g-ada", Email: "ada@gmail.com", EmailVerified: true}, ""); err != nil || !gmail.User.EmailVerified() {
		t.Errorf("FinishSocialSignIn(Gmail) = %+v, %v; want a verified account", gmail.User, err)
	}

	// The victim resets the password with the emailed code.
	if err := f.svc.RequestPasswordReset(requestCtx(), "victim@corp.example"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.ResetPassword(requestCtx(), "victim@corp.example", f.emails.last(t, "reset").code, "the victim's own password"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Authenticate(context.Background(), res.Token); !errors.Is(err, authlib.ErrUnauthenticated) {
		t.Errorf("attacker's session after the reset error = %v, want ErrUnauthenticated", err)
	}
	again, err := f.webSignIn(t, social.Google, attacker, "")
	if !errors.Is(err, authdomain.ErrSocialLinkRequired) || again.Token != "" {
		t.Errorf("attacker's Google sign-in after the reset = %+v, %v; want ErrSocialLinkRequired and no session", again.User, err)
	}
	if _, err := f.svc.Login(requestCtx(), "victim@corp.example", "the victim's own password"); err != nil {
		t.Errorf("Login(victim) error = %v", err)
	}
}

// TestSignedInOwnerVerifiesWithoutLosingTheirSignIn: the person signed in
// with the account's own Google identity who also proves the address keeps
// the identity.
func TestSignedInOwnerVerifiesWithoutLosingTheirSignIn(t *testing.T) {
	f := newSocialFixture(t)
	owner := socialtest.Claims{Subject: "g-owner", Email: "owner@corp.example", EmailVerified: true}
	res, err := f.webSignIn(t, social.Google, owner, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := f.principalCtx(t, res.Token)
	if err := f.svc.ResendVerification(requestCtx(), "owner@corp.example"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.VerifyEmail(ctx, "owner@corp.example", f.emails.last(t, "verify").code); err != nil {
		t.Fatal(err)
	}
	if identities, err := f.svc.ListIdentities(ctx); err != nil || len(identities) != 1 {
		t.Errorf("ListIdentities() after verifying signed in = %+v, %v; want the Google identity kept", identities, err)
	}
	if again, err := f.webSignIn(t, social.Google, owner, ""); err != nil || again.User.ID != res.User.ID || !again.User.EmailVerified() {
		t.Errorf("Google sign-in after verifying = %+v, %v", again.User, err)
	}
}
