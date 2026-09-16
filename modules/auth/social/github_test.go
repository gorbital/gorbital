package social_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"gorbital.dev/modules/auth/social"
	"gorbital.dev/modules/auth/social/socialtest"
)

const gitHubRedirect = "http://localhost:8080/v1/auth/github/callback"

func gitHub(t *testing.T, srv *socialtest.Server) *social.Provider {
	t.Helper()
	p, err := social.NewGitHub(social.GitHubConfig{ClientID: "Iv1.github-client", ClientSecret: "github-secret", Endpoints: srv.GitHubEndpoints()})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// gitHubStart returns the authorization URL's query for a new verifier.
func gitHubStart(t *testing.T, p *social.Provider, verifier string) url.Values {
	t.Helper()
	u, err := url.Parse(p.AuthCodeURL(gitHubRedirect, "state-1", "nonce-1", verifier))
	if err != nil {
		t.Fatal(err)
	}
	return u.Query()
}

func TestGitHubWebSignIn(t *testing.T) {
	srv := socialtest.New(t)
	p := gitHub(t, srv)
	ctx := context.Background()
	verifier := social.NewPKCEVerifier()

	q := gitHubStart(t, p, verifier)
	for key, want := range map[string]string{
		"client_id": "Iv1.github-client", "redirect_uri": gitHubRedirect, "response_type": "code",
		"scope": "read:user user:email", "state": "state-1", "code_challenge_method": "S256",
	} {
		if q.Get(key) != want {
			t.Errorf("AuthCodeURL() %s = %q, want %q", key, q.Get(key), want)
		}
	}
	if q.Get("code_challenge") == "" || q.Has("nonce") || q.Has("response_mode") {
		t.Errorf("AuthCodeURL() = %v, want a PKCE challenge, no nonce and no form_post", q)
	}
	if p.Name() != social.GitHub || !p.Web() || len(p.NativeClients()) != 0 {
		t.Errorf("provider = %s, web %t, native %v", p.Name(), p.Web(), p.NativeClients())
	}

	user := socialtest.GitHubUser{ID: 583231, Login: "octocat", Name: "The Octocat", Emails: []socialtest.GitHubEmail{
		{Email: "583231+octocat@users.noreply.github.com", Verified: true},
		{Email: "old@example.com", Verified: true},
		{Email: "octocat@example.com", Primary: true, Verified: true},
	}}
	code := srv.GitHubCode(user, q.Get("code_challenge"))
	tok, err := p.Exchange(ctx, gitHubRedirect, code, verifier, "ignored")
	want := social.Identity{Provider: social.GitHub, Subject: "583231", Email: "octocat@example.com", EmailVerified: true, Name: "The Octocat", Audience: "Iv1.github-client"}
	if err != nil || tok.Identity != want || tok.RefreshToken != "" {
		t.Fatalf("Exchange() = %+v, %v; want %+v", tok, err, want)
	}
	form := srv.TokenRequests()[0]
	if form.Get("code_verifier") != verifier || form.Get("client_secret") != "github-secret" || form.Get("redirect_uri") != gitHubRedirect {
		t.Errorf("token request = %v, want the verifier, secret and redirect URI", form)
	}
	if tok.Identity.AuthoritativeEmail() {
		t.Error("a GitHub identity is authoritative for its email, want never")
	}

	// A used code, another verifier (PKCE) and a code of nobody fail.
	if _, err := p.Exchange(ctx, gitHubRedirect, code, verifier, ""); !errors.Is(err, social.ErrExchange) {
		t.Errorf("Exchange(used code) error = %v, want ErrExchange", err)
	}
	stolen := srv.GitHubCode(user, q.Get("code_challenge"))
	if _, err := p.Exchange(ctx, gitHubRedirect, stolen, social.NewPKCEVerifier(), ""); !errors.Is(err, social.ErrExchange) {
		t.Errorf("Exchange(other verifier) error = %v, want ErrExchange", err)
	}
	if _, err := p.Exchange(ctx, gitHubRedirect, "made-up", verifier, ""); !errors.Is(err, social.ErrExchange) {
		t.Errorf("Exchange(unknown code) error = %v, want ErrExchange", err)
	}
}

// TestGitHubEmails: only the primary address counts, verified or not, and
// GitHub's no-reply addresses are none.
func TestGitHubEmails(t *testing.T) {
	srv := socialtest.New(t)
	p := gitHub(t, srv)
	ctx := context.Background()
	exchange := func(u socialtest.GitHubUser) (social.Identity, error) {
		t.Helper()
		verifier := social.NewPKCEVerifier()
		tok, err := p.Exchange(ctx, gitHubRedirect, srv.GitHubCode(u, gitHubStart(t, p, verifier).Get("code_challenge")), verifier, "")
		return tok.Identity, err
	}
	tests := []struct {
		name         string
		user         socialtest.GitHubUser
		email        string
		verified     bool
		wantName     string
		wantExchange bool
	}{
		{"unverified primary", socialtest.GitHubUser{ID: 1, Login: "a", Emails: []socialtest.GitHubEmail{
			{Email: "a@example.com", Primary: true}, {Email: "b@example.com", Verified: true},
		}}, "a@example.com", false, "a", false},
		{"no emails", socialtest.GitHubUser{ID: 2, Login: "b"}, "", false, "b", false},
		{"no-reply primary", socialtest.GitHubUser{ID: 3, Login: "c", Name: "  C  ", Emails: []socialtest.GitHubEmail{
			{Email: "3+c@Users.NoReply.GitHub.com", Primary: true, Verified: true},
		}}, "", false, "C", false},
		{"emails refused (no user:email scope)", socialtest.GitHubUser{ID: 4, Login: "d", EmailsStatus: http.StatusForbidden}, "", false, "", true},
		{"no user ID", socialtest.GitHubUser{Login: "e", Emails: []socialtest.GitHubEmail{{Email: "e@example.com", Primary: true, Verified: true}}}, "", false, "", true},
	}
	for _, tt := range tests {
		id, err := exchange(tt.user)
		switch {
		case tt.wantExchange:
			if !errors.Is(err, social.ErrExchange) {
				t.Errorf("%s: Exchange() error = %v, want ErrExchange", tt.name, err)
			}
		case err != nil || id.Email != tt.email || id.EmailVerified != tt.verified || id.Name != tt.wantName:
			t.Errorf("%s: Exchange() = %+v, %v; want email %q verified %t name %q", tt.name, id, err, tt.email, tt.verified, tt.wantName)
		}
	}
}

func TestGitHubHasNoTokensOrNativeFlow(t *testing.T) {
	srv := socialtest.New(t)
	p := gitHub(t, srv)
	ctx := context.Background()
	if _, err := p.VerifyIDToken(ctx, srv.IDToken(socialtest.Claims{Subject: "1", Audience: "Iv1.github-client", Nonce: "n"}), "n"); !errors.Is(err, social.ErrInvalidToken) || !errors.Is(err, social.ErrNotSupported) {
		t.Errorf("VerifyIDToken() error = %v, want ErrInvalidToken and ErrNotSupported", err)
	}
	if _, err := p.ExchangeNativeCode(ctx, "code", "Iv1.github-client"); !errors.Is(err, social.ErrNotSupported) {
		t.Errorf("ExchangeNativeCode() error = %v, want ErrNotSupported", err)
	}
	if err := p.Revoke(ctx, "token", "Iv1.github-client"); !errors.Is(err, social.ErrNotSupported) {
		t.Errorf("Revoke() error = %v, want ErrNotSupported", err)
	}
	if _, err := p.AppleNotification(ctx, "payload"); !errors.Is(err, social.ErrInvalidConfig) {
		t.Errorf("AppleNotification() error = %v, want ErrInvalidConfig", err)
	}
	for _, c := range []social.GitHubConfig{{ClientID: "id"}, {ClientSecret: "secret"}} {
		if _, err := social.NewGitHub(c); !errors.Is(err, social.ErrInvalidConfig) {
			t.Errorf("NewGitHub(%+v) error = %v, want ErrInvalidConfig", c, err)
		}
	}
	if def := social.GitHubEndpoints(); def.APIURL != "https://api.github.com" || def.AuthURL != "https://github.com/login/oauth/authorize" {
		t.Errorf("GitHubEndpoints() = %+v", def)
	}
}
