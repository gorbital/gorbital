# modules/auth/social/socialtest

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/modules/auth/social/socialtest"
```

Package socialtest runs an in-process OpenID Connect provider standing in for Google or Apple in tests: it serves signing keys, a token endpoint and a revocation endpoint, and issues signed ID tokens, authorization codes and Apple notifications. Point a social provider at it with Endpoints. It also stands in for GitHub (GitHubEndpoints): an OAuth token endpoint checking PKCE, and the user and email API.

Stability: stable, for tests only: the API follows the compatibility promise; what the helpers do inside a test may change (ADR-0015, ADR-0054).

## Contents

- Types:
  - [`Claims`](#Claims)
  - [`GitHubEmail`](#GitHubEmail)
  - [`GitHubUser`](#GitHubUser)
  - [`Server`](#Server): [`New`](#New), [`Server.Code`](#Server.Code), [`Server.Endpoints`](#Server.Endpoints), [`Server.FailRevocations`](#Server.FailRevocations), [`Server.GitHubCode`](#Server.GitHubCode), [`Server.GitHubEndpoints`](#Server.GitHubEndpoints), [`Server.IDToken`](#Server.IDToken), [`Server.Notification`](#Server.Notification), [`Server.Revoked`](#Server.Revoked), [`Server.TokenRequests`](#Server.TokenRequests)

## Types

<a id="Claims"></a>
<a id="Claims.Subject"></a>
<a id="Claims.Audience"></a>
<a id="Claims.Email"></a>
<a id="Claims.EmailVerified"></a>
<a id="Claims.PrivateEmail"></a>
<a id="Claims.Nonce"></a>
<a id="Claims.Name"></a>
<a id="Claims.IssuedAt"></a>
<a id="Claims.Issuer"></a>
<a id="Claims.Extra"></a>

### type Claims

```go
type Claims struct {
	Subject       string
	Audience      string
	Email         string
	EmailVerified bool
	PrivateEmail  bool
	Nonce         string
	Name          string
	// IssuedAt defaults to the server's Now; Issuer to its URL.
	IssuedAt time.Time
	Issuer   string
	// Extra adds or replaces claims; a nil value removes one.
	Extra map[string]any
}
```

Claims describe an ID token to issue.

*Since `v0.1.0`*

<a id="GitHubEmail"></a>
<a id="GitHubEmail.Email"></a>
<a id="GitHubEmail.Primary"></a>
<a id="GitHubEmail.Verified"></a>

### type GitHubEmail

```go
type GitHubEmail struct {
	Email    string
	Primary  bool
	Verified bool
}
```

GitHubEmail is one of a GitHub account's email addresses.

*Since `v0.1.0`*

<a id="GitHubUser"></a>
<a id="GitHubUser.ID"></a>
<a id="GitHubUser.Login"></a>
<a id="GitHubUser.Name"></a>
<a id="GitHubUser.Emails"></a>
<a id="GitHubUser.EmailsStatus"></a>

### type GitHubUser

```go
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
```

GitHubUser describes a GitHub account for GitHubCode.

*Since `v0.1.0`*

<a id="Server"></a>
<a id="Server.URL"></a>
<a id="Server.Now"></a>

### type Server

```go
type Server struct {
	// URL is the server's base URL and issuer.
	URL string
	// Now is the clock for issued tokens. Default: time.Now.
	Now func() time.Time
	// contains filtered or unexported fields
}
```

Server is a fake provider. It is safe for concurrent use.

*Since `v0.1.0`*

<a id="New"></a>

#### func New

```go
func New(t testing.TB) *Server
```

New starts a server, closed when the test ends.

*Since `v0.1.0`*

<a id="Server.Code"></a>

#### func (*Server) Code

```go
func (s *Server) Code(c Claims, refreshToken string) string
```

Code returns a single-use authorization code whose exchange returns an ID token with c's claims and refreshToken (none when empty).

*Since `v0.1.0`*

<a id="Server.Endpoints"></a>

#### func (*Server) Endpoints

```go
func (s *Server) Endpoints() social.Endpoints
```

Endpoints returns the server's endpoints for a social provider.

*Since `v0.1.0`*

<a id="Server.FailRevocations"></a>

#### func (*Server) FailRevocations

```go
func (s *Server) FailRevocations(fail bool)
```

FailRevocations makes the revocation endpoint answer 503 while fail is true, to test retries.

*Since `v0.1.0`*

<a id="Server.GitHubCode"></a>

#### func (*Server) GitHubCode

```go
func (s *Server) GitHubCode(user GitHubUser, codeChallenge string) string
```

GitHubCode returns a single-use GitHub authorization code for user. When codeChallenge is set (the code\_challenge of the authorization URL), the token request must carry the PKCE verifier whose S256 challenge it is.

*Since `v0.1.0`*

<a id="Server.GitHubEndpoints"></a>

#### func (*Server) GitHubEndpoints

```go
func (s *Server) GitHubEndpoints() social.Endpoints
```

GitHubEndpoints returns the server's endpoints for the GitHub provider.

*Since `v0.1.0`*

<a id="Server.IDToken"></a>

#### func (*Server) IDToken

```go
func (s *Server) IDToken(c Claims) string
```

IDToken returns a signed ID token with c's claims, valid for an hour.

*Since `v0.1.0`*

<a id="Server.Notification"></a>

#### func (*Server) Notification

```go
func (s *Server) Notification(audience, eventType, subject string) string
```

Notification returns a signed Apple server-to-server notification.

*Since `v0.1.0`*

<a id="Server.Revoked"></a>

#### func (*Server) Revoked

```go
func (s *Server) Revoked() []string
```

Revoked returns every token revoked.

*Since `v0.1.0`*

<a id="Server.TokenRequests"></a>

#### func (*Server) TokenRequests

```go
func (s *Server) TokenRequests() []url.Values
```

TokenRequests returns the form of every token request received.

*Since `v0.1.0`*
