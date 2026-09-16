package socialtest

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"gorbital.dev/modules/auth/social"
)

// GitHubUser describes a GitHub account for GitHubCode.
type GitHubUser struct {
	// ID is the numeric user ID, the identity's subject.
	ID    int64
	Login string
	Name  string
	// Emails are what GET /user/emails returns.
	Emails []GitHubEmail
	// EmailsStatus, when set, is the status GET /user/emails answers with
	// instead, such as 403 for a token without the user:email scope.
	EmailsStatus int
}

// GitHubEmail is one of a GitHub account's email addresses.
type GitHubEmail struct {
	Email    string
	Primary  bool
	Verified bool
}

type gitHubGrant struct {
	user          GitHubUser
	codeChallenge string
}

// GitHubEndpoints returns the server's endpoints for the GitHub provider.
func (s *Server) GitHubEndpoints() social.Endpoints {
	return social.Endpoints{AuthURL: s.URL + "/login/oauth/authorize", TokenURL: s.URL + "/login/oauth/access_token", APIURL: s.URL}
}

// GitHubCode returns a single-use GitHub authorization code for user. When
// codeChallenge is set (the code_challenge of the authorization URL), the
// token request must carry the PKCE verifier whose S256 challenge it is.
func (s *Server) GitHubCode(user GitHubUser, codeChallenge string) string {
	code := random()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gitHubGrants[code] = gitHubGrant{user: user, codeChallenge: codeChallenge}
	return code
}

// serveGitHubToken answers like GitHub's token endpoint without an Accept
// header: form-encoded, with errors reported in a 200 response.
func (s *Server) serveGitHubToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	code := r.PostForm.Get("code")
	s.mu.Lock()
	s.requests = append(s.requests, r.PostForm)
	g, ok := s.gitHubGrants[code]
	delete(s.gitHubGrants, code)
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/x-www-form-urlencoded")
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	switch {
	case r.PostForm.Get("client_secret") == "":
		_, _ = w.Write([]byte(url.Values{"error": {"incorrect_client_credentials"}}.Encode()))
		return
	case !ok || (g.codeChallenge != "" && subtle.ConstantTimeCompare([]byte(challenge), []byte(g.codeChallenge)) != 1):
		_, _ = w.Write([]byte(url.Values{"error": {"bad_verification_code"}, "error_description": {"The code passed is incorrect or expired."}}.Encode()))
		return
	}
	token := "gho_" + random()
	s.mu.Lock()
	s.gitHubTokens[token] = g.user
	s.mu.Unlock()
	_, _ = w.Write([]byte(url.Values{"access_token": {token}, "scope": {"read:user,user:email"}, "token_type": {"bearer"}}.Encode()))
}

func (s *Server) gitHubUser(w http.ResponseWriter, r *http.Request) (GitHubUser, bool) {
	token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	s.mu.Lock()
	u, ok := s.gitHubTokens[token]
	s.mu.Unlock()
	ok = ok && r.Header.Get("Accept") == "application/vnd.github+json"
	w.Header().Set("Content-Type", "application/json")
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
	}
	return u, ok
}

func (s *Server) serveGitHubUser(w http.ResponseWriter, r *http.Request) {
	u, ok := s.gitHubUser(w, r)
	if !ok {
		return
	}
	body := map[string]any{"id": u.ID, "login": u.Login, "name": nil, "email": nil}
	if u.Name != "" {
		body["name"] = u.Name
	}
	_ = json.NewEncoder(w).Encode(body)
}

func (s *Server) serveGitHubEmails(w http.ResponseWriter, r *http.Request) {
	u, ok := s.gitHubUser(w, r)
	if !ok {
		return
	}
	if u.EmailsStatus != 0 {
		w.WriteHeader(u.EmailsStatus)
		_, _ = w.Write([]byte(`{"message":"Resource not accessible by integration"}`))
		return
	}
	emails := make([]map[string]any, len(u.Emails))
	for i, e := range u.Emails {
		emails[i] = map[string]any{"email": e.Email, "primary": e.Primary, "verified": e.Verified, "visibility": nil}
	}
	_ = json.NewEncoder(w).Encode(emails)
}
