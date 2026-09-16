package social

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// gitHubAPIVersion is the REST API version requested, so a new default
	// version can't change the responses read here.
	gitHubAPIVersion = "2022-11-28"
	// maxGitHubResponse bounds what is read from GitHub's API.
	maxGitHubResponse = 1 << 20
	// gitHubNoReplyDomain is the domain of GitHub's private commit addresses,
	// which receive no email.
	gitHubNoReplyDomain = "users.noreply.github.com"
)

// GitHubEndpoints returns GitHub's endpoints.
func GitHubEndpoints() Endpoints {
	return Endpoints{ //nolint:gosec // public URLs, not credentials
		AuthURL:  "https://github.com/login/oauth/authorize",
		TokenURL: "https://github.com/login/oauth/access_token",
		APIURL:   "https://api.github.com",
	}
}

// GitHubConfig configures GitHub sign-in with an OAuth app.
type GitHubConfig struct {
	// ClientID and ClientSecret are the OAuth app's.
	ClientID     string
	ClientSecret string
	Endpoints    Endpoints
	HTTPClient   *http.Client
}

// NewGitHub returns the GitHub provider (ADR-0059). GitHub has no OpenID
// Connect: [Provider.Exchange] trades the code (with state and PKCE, like
// Google) for an access token, reads the person from GET /user and their
// primary email from GET /user/emails with the scopes read:user and
// user:email, and forgets the token. It offers the web flow only: ID tokens,
// native codes and revocation return [ErrNotSupported]. It returns
// [ErrInvalidConfig].
func NewGitHub(c GitHubConfig) (*Provider, error) {
	if c.ClientID == "" || c.ClientSecret == "" {
		return nil, fmt.Errorf("%w: GitHub needs the OAuth app's client ID and secret", ErrInvalidConfig)
	}
	secret := c.ClientSecret
	p := newProvider(GitHub, withDefaults(c.Endpoints, GitHubEndpoints()), c.HTTPClient, nil)
	p.webClient = c.ClientID
	p.clients = []string{c.ClientID}
	p.secret = func(string) (string, error) { return secret, nil }
	p.scopes, p.pkce = []string{"read:user", "user:email"}, true
	return p, nil
}

// gitHubIdentity reads the person an access token belongs to. Email is the
// primary address, and EmailVerified whether GitHub verified it; a primary
// address at users.noreply.github.com, which receives no email, counts as
// none. It returns [ErrExchange].
func (p *Provider) gitHubIdentity(ctx context.Context, accessToken string) (Identity, error) {
	var user struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Name  string `json:"name"`
	}
	if err := p.gitHubGet(ctx, accessToken, "/user", &user); err != nil {
		return Identity{}, err
	}
	if user.ID <= 0 {
		return Identity{}, fmt.Errorf("%w: GitHub returned no user ID", ErrExchange)
	}
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := p.gitHubGet(ctx, accessToken, "/user/emails", &emails); err != nil {
		return Identity{}, err
	}
	id := Identity{
		Provider: GitHub, Subject: strconv.FormatInt(user.ID, 10), Audience: p.webClient,
		Name: strings.TrimSpace(user.Name),
	}
	if id.Name == "" {
		id.Name = strings.TrimSpace(user.Login)
	}
	for _, e := range emails {
		email := strings.TrimSpace(e.Email)
		if !e.Primary || email == "" || strings.HasSuffix(strings.ToLower(email), "@"+gitHubNoReplyDomain) {
			continue
		}
		id.Email, id.EmailVerified = email, e.Verified
		break
	}
	return id, nil
}

// gitHubGet reads path from GitHub's REST API into out.
func (p *Provider) gitHubGet(ctx context.Context, accessToken, path string, out any) error {
	if accessToken == "" {
		return fmt.Errorf("%w: GitHub returned no access token", ErrExchange)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(p.ep.APIURL, "/")+path, nil)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrExchange, err) //nolint:errorlint // not API
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", gitHubAPIVersion)
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: GitHub %s: %v", ErrExchange, path, err) //nolint:errorlint // not API
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: GitHub %s: %s", ErrExchange, path, resp.Status)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxGitHubResponse)).Decode(out); err != nil {
		return fmt.Errorf("%w: GitHub %s: %v", ErrExchange, path, err) //nolint:errorlint // not API
	}
	return nil
}
