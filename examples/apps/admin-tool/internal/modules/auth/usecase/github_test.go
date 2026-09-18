package usecase_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"gorbital.dev/config"
	authlib "gorbital.dev/modules/auth"
	"gorbital.dev/modules/auth/social"
	"gorbital.dev/modules/auth/social/socialtest"

	authdomain "example.com/admin-tool/internal/modules/auth/domain"
	authusecase "example.com/admin-tool/internal/modules/auth/usecase"
)

// gitHubUser is a GitHub account whose primary address is email.
func gitHubUser(id int64, login, email string, verified bool) socialtest.GitHubUser {
	return socialtest.GitHubUser{ID: id, Login: login, Emails: []socialtest.GitHubEmail{
		{Email: fmt.Sprintf("%d+%s@users.noreply.github.com", id, login), Verified: true},
		{Email: email, Primary: true, Verified: verified},
	}}
}

// finishGitHub finishes a started GitHub flow as GitHub would for user, in
// the browser holding browser.
func (f *socialFixture) finishGitHub(t *testing.T, start authusecase.SocialStart, browser string, user socialtest.GitHubUser) (authusecase.SocialResult, error) {
	t.Helper()
	q := query(t, start.URL)
	if browser == "" {
		browser = start.BrowserToken
	}
	return f.svc.FinishSocialSignIn(requestCtx(), social.GitHub, q.Get("state"), browser, f.srv.GitHubCode(user, q.Get("code_challenge")), "")
}

// gitHubSignIn signs in with GitHub in a browser.
func (f *socialFixture) gitHubSignIn(t *testing.T, user socialtest.GitHubUser) (authusecase.SocialResult, error) {
	t.Helper()
	start, err := f.svc.StartSocialSignIn(requestCtx(), social.GitHub, returnTo)
	if err != nil {
		t.Fatalf("StartSocialSignIn(github) error = %v", err)
	}
	return f.finishGitHub(t, start, "", user)
}

// TestGitHubSignIn: GitHub signs people in with its verified primary email,
// never verifies an address and never links an existing account (ADR-0059).
func TestGitHubSignIn(t *testing.T) {
	f := newSocialFixture(t, func(c *authusecase.Config) { c.UnverifiedAccountTTL = config.Static(48 * time.Hour) })

	start, err := f.svc.StartSocialSignIn(requestCtx(), social.GitHub, "")
	q := query(t, start.URL)
	if err != nil || q.Get("redirect_uri") != "http://localhost:8080/v1/auth/github/callback" || q.Get("code_challenge") == "" || q.Get("scope") != "read:user user:email" {
		t.Fatalf("StartSocialSignIn(github) = %s, %v", start.URL, err)
	}

	// A new person gets an account without a password, not verified: GitHub
	// hosts nobody's email.
	octo := gitHubUser(583231, "octocat", "octocat@gmail.com", true)
	res, err := f.gitHubSignIn(t, octo)
	if err != nil || res.Token == "" || res.Linked || res.User.EmailVerified() || res.User.HasPassword() || res.User.Email != "octocat@gmail.com" {
		t.Fatalf("FinishSocialSignIn(new GitHub person) = %+v, %v", res, err)
	}
	identities, err := f.svc.ListIdentities(f.principalCtx(t, res.Token))
	if err != nil || len(identities) != 1 || identities[0].Provider != social.GitHub || identities[0].Subject != "583231" || identities[0].Name != "octocat" {
		t.Fatalf("ListIdentities() = %+v, %v", identities, err)
	}
	if e, _ := f.audit.find("auth.login.succeeded"); e.Metadata["method"] != social.GitHub {
		t.Errorf("sign-in audit event = %+v, want method github", e)
	}

	// The same GitHub user ID signs in to the same account, whatever its login
	// and email are now.
	renamed := gitHubUser(583231, "octocat-renamed", "new@example.com", false)
	if again, err := f.gitHubSignIn(t, renamed); err != nil || again.User.ID != res.User.ID {
		t.Fatalf("FinishSocialSignIn(returning GitHub person) = %+v, %v", again.User, err)
	}

	// A new person without a verified primary email is refused, with nothing
	// written.
	for name, user := range map[string]socialtest.GitHubUser{
		"unverified primary": gitHubUser(2, "unverified", "unverified@example.com", false),
		"no email":           {ID: 3, Login: "noemail"},
	} {
		if _, err := f.gitHubSignIn(t, user); !errors.Is(err, authdomain.ErrSocialEmailUnverified) {
			t.Errorf("%s: FinishSocialSignIn() error = %v, want ErrSocialEmailUnverified", name, err)
		}
	}
	if _, err := f.svc.UserByEmail(operator(), "unverified@example.com"); !errors.Is(err, authdomain.ErrUserNotFound) {
		t.Errorf("UserByEmail(refused GitHub person) error = %v, want ErrUserNotFound", err)
	}
	if e, _ := f.audit.find("auth.login.failed"); e.Metadata["reason"] != "social_email_unverified" {
		t.Errorf("refusal audit event = %+v", e)
	}

	// An address with an account is never linked by GitHub, verified or not,
	// even on a domain other providers are authoritative for.
	f.signUp(t, "ada@gmail.com")
	if err := f.svc.Register(requestCtx(), "eve@example.com", password); err != nil {
		t.Fatal(err)
	}
	for _, email := range []string{"ada@gmail.com", "eve@example.com"} {
		if _, err := f.gitHubSignIn(t, gitHubUser(10+int64(len(email)), "x", email, true)); !errors.Is(err, authdomain.ErrSocialLinkRequired) {
			t.Errorf("FinishSocialSignIn(GitHub, address of an account %s) error = %v, want ErrSocialLinkRequired", email, err)
		}
		if u, err := f.svc.UserByEmail(operator(), email); err != nil || !u.HasPassword() {
			t.Errorf("account %s after GitHub = %+v, %v; want it unchanged", email, u, err)
		}
	}
	if _, err := f.svc.Login(requestCtx(), "ada@gmail.com", password); err != nil {
		t.Errorf("Login(ada) after GitHub error = %v", err)
	}

	// A code for another PKCE challenge (a stolen code) fails.
	start, _ = f.svc.StartSocialSignIn(requestCtx(), social.GitHub, returnTo)
	q = query(t, start.URL)
	stolen := f.srv.GitHubCode(octo, "another-challenge")
	if _, err := f.svc.FinishSocialSignIn(requestCtx(), social.GitHub, q.Get("state"), start.BrowserToken, stolen, ""); !errors.Is(err, authdomain.ErrInvalidSocialToken) {
		t.Errorf("FinishSocialSignIn(code for another verifier) error = %v, want ErrInvalidSocialToken", err)
	}
	// Another browser, or a Google state, doesn't finish it.
	start, _ = f.svc.StartSocialSignIn(requestCtx(), social.GitHub, returnTo)
	if _, err := f.finishGitHub(t, start, "another-browser", octo); !errors.Is(err, authdomain.ErrInvalidState) {
		t.Errorf("FinishSocialSignIn(other browser) error = %v, want ErrInvalidState", err)
	}
	google, _ := f.svc.StartSocialSignIn(requestCtx(), social.Google, returnTo)
	if _, err := f.finishGitHub(t, google, "", octo); !errors.Is(err, authdomain.ErrInvalidState) {
		t.Errorf("FinishSocialSignIn(Google's state at GitHub's callback) error = %v, want ErrInvalidState", err)
	}

	// GitHub has no native flow.
	if _, _, err := f.svc.SocialNonce(requestCtx(), social.GitHub); !errors.Is(err, authdomain.ErrSocialUnavailable) {
		t.Errorf("SocialNonce(github) error = %v, want ErrSocialUnavailable", err)
	}
	if _, err := f.svc.SignInWithIDToken(requestCtx(), social.GitHub, "token", "nonce", "", ""); !errors.Is(err, authdomain.ErrSocialUnavailable) {
		t.Errorf("SignInWithIDToken(github) error = %v, want ErrSocialUnavailable", err)
	}

	// The unverified account holding its GitHub identity isn't expired; a
	// plain unverified registration is.
	f.clock.advance(49 * time.Hour)
	if _, err := f.svc.Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.UserByEmail(operator(), "octocat@gmail.com"); err != nil {
		t.Errorf("UserByEmail(GitHub account) after cleanup error = %v, want it kept", err)
	}
	if _, err := f.svc.UserByEmail(operator(), "eve@example.com"); !errors.Is(err, authdomain.ErrUserNotFound) {
		t.Errorf("UserByEmail(unverified registration) after cleanup error = %v, want ErrUserNotFound", err)
	}
}

// TestGitHubAccountIsClaimedByEmail: whoever proves the address of an
// account GitHub created removes the GitHub identity (ADR-0046 follow-up).
func TestGitHubAccountIsClaimedByEmail(t *testing.T) {
	f := newSocialFixture(t)
	attacker := gitHubUser(666, "attacker", "victim@corp.example", true)
	res, err := f.gitHubSignIn(t, attacker)
	if err != nil || res.User.EmailVerified() {
		t.Fatalf("FinishSocialSignIn(GitHub) = %+v, %v", res.User, err)
	}
	if err := f.svc.RequestPasswordReset(requestCtx(), "victim@corp.example"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.ResetPassword(requestCtx(), "victim@corp.example", f.emails.last(t, "reset").code, "the victim's own password"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Authenticate(context.Background(), res.Token); !errors.Is(err, authlib.ErrUnauthenticated) {
		t.Errorf("attacker's session after the reset error = %v, want ErrUnauthenticated", err)
	}
	if _, err := f.gitHubSignIn(t, attacker); !errors.Is(err, authdomain.ErrSocialLinkRequired) {
		t.Errorf("attacker's GitHub sign-in after the reset error = %v, want ErrSocialLinkRequired", err)
	}
}

// TestGitHubLinkWhileSignedIn: a signed-in user links GitHub through the web
// flow, bound to the browser and to the session that started it (ADR-0059).
func TestGitHubLinkWhileSignedIn(t *testing.T) {
	f := newSocialFixture(t)
	f.signUp(t, "ada@corp.example")
	ctx, login := f.login(t, "ada@corp.example")
	ada := gitHubUser(1815, "ada", "ada@corp.example", true)

	// The user is checked first, and only GitHub links this way.
	if _, err := f.svc.StartIdentityLink(ctx, social.GitHub, returnTo, "wrong password"); !errors.Is(err, authdomain.ErrInvalidCredentials) {
		t.Errorf("StartIdentityLink(wrong password) error = %v, want ErrInvalidCredentials", err)
	}
	if e, _ := f.audit.find("auth.reauth.failed"); e.ResourceID != login.User.ID {
		t.Errorf("reauth audit event = %+v", e)
	}
	if _, err := f.svc.StartIdentityLink(ctx, social.Google, returnTo, password); !errors.Is(err, authdomain.ErrSocialUnavailable) {
		t.Errorf("StartIdentityLink(google) error = %v, want ErrSocialUnavailable", err)
	}
	if _, err := f.svc.StartIdentityLink(ctx, social.GitHub, "https://evil.example/", password); !errors.Is(err, authdomain.ErrInvalidReturnTo) {
		t.Errorf("StartIdentityLink(other site) error = %v, want ErrInvalidReturnTo", err)
	}
	if _, err := f.svc.StartIdentityLink(requestCtx(), social.GitHub, returnTo, password); !errors.Is(err, authlib.ErrUnauthenticated) {
		t.Errorf("StartIdentityLink(signed out) error = %v, want ErrUnauthenticated", err)
	}
	key := authlib.WithPrincipal(requestCtx(), authlib.Principal{UserID: login.User.ID, APIKeyID: "key_1"})
	if _, err := f.svc.StartIdentityLink(key, social.GitHub, returnTo, password); !errors.Is(err, authdomain.ErrSessionRequired) {
		t.Errorf("StartIdentityLink(API key) error = %v, want ErrSessionRequired", err)
	}
	if _, _, err := f.svc.LinkIdentity(ctx, social.GitHub, "token", "nonce", "", "", password); !errors.Is(err, authdomain.ErrSocialUnavailable) {
		t.Errorf("LinkIdentity(github ID token) error = %v, want ErrSocialUnavailable", err)
	}

	// A callback in another browser (a link someone sent) doesn't link, and
	// uses the flow up.
	start, err := f.svc.StartIdentityLink(ctx, social.GitHub, returnTo, password)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.finishGitHub(t, start, "attacker-browser", gitHubUser(666, "attacker", "a@example.com", true)); !errors.Is(err, authdomain.ErrInvalidState) {
		t.Errorf("FinishSocialSignIn(link in another browser) error = %v, want ErrInvalidState", err)
	}
	if _, err := f.finishGitHub(t, start, "", ada); !errors.Is(err, authdomain.ErrInvalidState) {
		t.Errorf("FinishSocialSignIn(used link) error = %v, want ErrInvalidState", err)
	}

	start, err = f.svc.StartIdentityLink(ctx, social.GitHub, returnTo, password)
	if err != nil {
		t.Fatal(err)
	}
	res, err := f.finishGitHub(t, start, "", ada)
	if err != nil || !res.Linked || res.Token != "" || res.Challenge != nil || res.ReturnTo != returnTo {
		t.Fatalf("FinishSocialSignIn(link) = %+v, %v; want linked, no session", res, err)
	}
	identities, _ := f.svc.ListIdentities(ctx)
	if len(identities) != 1 || identities[0].Provider != social.GitHub || f.emails.count("sign_in_method_added") != 1 {
		t.Fatalf("ListIdentities() after linking = %+v, %d emails", identities, f.emails.count("sign_in_method_added"))
	}
	if e, _ := f.audit.find("auth.identity.linked"); e.ResourceID != login.User.ID || e.Metadata["flow"] != "web" || e.Metadata["provider"] != social.GitHub {
		t.Errorf("link audit event = %+v", e)
	}
	if signedIn, err := f.gitHubSignIn(t, ada); err != nil || signedIn.User.ID != login.User.ID {
		t.Errorf("GitHub sign-in after linking = %+v, %v; want the owner's account", signedIn.User, err)
	}
	// Linking it again changes nothing.
	start, _ = f.svc.StartIdentityLink(ctx, social.GitHub, returnTo, password)
	if res, err := f.finishGitHub(t, start, "", ada); err != nil || !res.Linked || f.emails.count("sign_in_method_added") != 1 {
		t.Errorf("FinishSocialSignIn(link again) = %+v, %v", res, err)
	}

	// Another account's GitHub identity can't be linked.
	if _, err := f.gitHubSignIn(t, gitHubUser(99, "bob", "bob@example.com", true)); err != nil {
		t.Fatal(err)
	}
	start, _ = f.svc.StartIdentityLink(ctx, social.GitHub, returnTo, password)
	if _, err := f.finishGitHub(t, start, "", gitHubUser(99, "bob", "bob@example.com", true)); !errors.Is(err, authdomain.ErrIdentityInUse) {
		t.Errorf("FinishSocialSignIn(link another account's GitHub) error = %v, want ErrIdentityInUse", err)
	}

	// Signing out cancels a started link.
	start, _ = f.svc.StartIdentityLink(ctx, social.GitHub, returnTo, password)
	if err := f.svc.Logout(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.finishGitHub(t, start, "", gitHubUser(100, "ada-work", "ada@work.example", true)); !errors.Is(err, authlib.ErrUnauthenticated) {
		t.Errorf("FinishSocialSignIn(link after signing out) error = %v, want ErrUnauthenticated", err)
	}

	// Unlinking needs the password, like other providers. (Each start above
	// spent the reauthentication budget.)
	f.clock.advance(time.Hour)
	ctx, _ = f.login(t, "ada@corp.example")
	identities, _ = f.svc.ListIdentities(ctx)
	if len(identities) != 1 {
		t.Fatalf("ListIdentities() = %+v, want only GitHub", identities)
	}
	if err := f.svc.RemoveIdentity(ctx, identities[0].ID, "wrong password"); !errors.Is(err, authdomain.ErrInvalidCredentials) {
		t.Errorf("RemoveIdentity(wrong password) error = %v", err)
	}
	if err := f.svc.RemoveIdentity(ctx, identities[0].ID, password); err != nil {
		t.Fatalf("RemoveIdentity() error = %v", err)
	}
	if _, err := f.gitHubSignIn(t, ada); !errors.Is(err, authdomain.ErrSocialLinkRequired) {
		t.Errorf("GitHub sign-in after unlinking error = %v, want ErrSocialLinkRequired", err)
	}
}
