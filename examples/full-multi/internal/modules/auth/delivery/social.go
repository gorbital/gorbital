package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	authlib "gorbital.dev/modules/auth"

	authdomain "example.com/acme-api/internal/modules/auth/domain"
	authusecase "example.com/acme-api/internal/modules/auth/usecase"
)

// oauthCookie holds the browser's half of a web sign-in (ADR-0046).
const oauthCookie = "__Host-oauth"

// IdentityResponse is a Google or Apple account linked to the user.
type IdentityResponse struct {
	ID           string     `json:"id" example:"idn_nbswy3dpeb3w64tmmq"`
	Provider     string     `json:"provider" enum:"google,apple"`
	Email        string     `json:"email,omitempty" example:"ada@example.com"`
	PrivateEmail bool       `json:"private_email" doc:"An Apple relay address that forwards to the person's real one"`
	Name         string     `json:"name,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	LastUsedAt   *time.Time `json:"last_used_at,omitempty"`
}

// IdentityList is the user's linked Google and Apple accounts, oldest first.
type IdentityList struct {
	Identities []IdentityResponse `json:"identities"`
}

// SocialNonceResponse is a nonce for a native sign-in request.
type SocialNonceResponse struct {
	Nonce     string    `json:"nonce" doc:"Put it in the SDK's sign-in request (Apple's iOS SDK takes its SHA-256 in hex), then send it with the ID token. It works once."`
	ExpiresAt time.Time `json:"expires_at"`
}

type redirectOutput struct {
	Status    int
	Location  string        `header:"Location"`
	SetCookie []http.Cookie `header:"Set-Cookie"`
}

type identityListOutput struct{ Body IdentityList }

type socialNonceOutput struct{ Body SocialNonceResponse }

type providerInput struct {
	Provider string `path:"provider" enum:"google,apple"`
}

type startSocialInput struct {
	Provider string `path:"provider" enum:"google,apple"`
	ReturnTo string `query:"return_to" maxLength:"2048" doc:"Where to send the browser afterwards: an absolute URL on the API's origin or an APP_CORS_ORIGINS origin. Default: the API docs"`
}

type googleCallbackInput struct {
	Code    string `query:"code" maxLength:"4096"`
	State   string `query:"state" maxLength:"256"`
	Error   string `query:"error" maxLength:"256"`
	Browser string `cookie:"__Host-oauth" doc:"Set by the start endpoint"`
}

type appleCallbackInput struct {
	Browser string `cookie:"__Host-oauth" doc:"Set by the start endpoint"`
	RawBody []byte `contentType:"application/x-www-form-urlencoded"`
}

type googleTokenInput struct {
	Body struct {
		_         struct{} `json:"-" additionalProperties:"true"`
		IDToken   string   `json:"id_token" maxLength:"16384" doc:"The ID token from Google's SDK"`
		Nonce     string   `json:"nonce" maxLength:"256" doc:"From POST /v1/auth/google/nonce, as put in the request"`
		Transport string   `json:"transport,omitempty" enum:"cookie,bearer" default:"bearer" doc:"bearer (native apps): the token in the response; cookie: an HttpOnly session cookie"`
	}
}

type appleTokenInput struct {
	Body struct {
		_                 struct{} `json:"-" additionalProperties:"true"`
		IDToken           string   `json:"id_token" maxLength:"16384" doc:"identityToken from ASAuthorizationAppleIDCredential"`
		Nonce             string   `json:"nonce" maxLength:"256" doc:"From POST /v1/auth/apple/nonce, before hashing"`
		AuthorizationCode string   `json:"authorization_code,omitempty" maxLength:"4096" doc:"authorizationCode, exchanged for a refresh token revoked when the account is deleted"`
		Name              string   `json:"name,omitempty" maxLength:"200" doc:"fullName, which Apple gives only the first time"`
		Transport         string   `json:"transport,omitempty" enum:"cookie,bearer" default:"bearer" doc:"bearer (native apps): the token in the response; cookie: an HttpOnly session cookie"`
	}
}

type identityOutput struct {
	Status int
	Body   IdentityResponse
}

type linkIdentityInput struct {
	Body struct {
		_                 struct{} `json:"-" additionalProperties:"true"`
		Provider          string   `json:"provider" enum:"google,apple"`
		IDToken           string   `json:"id_token" maxLength:"16384" doc:"The ID token from Google's or Apple's SDK, or from Google Identity Services or Sign in with Apple JS in a browser"`
		Nonce             string   `json:"nonce" maxLength:"256" doc:"From POST /v1/auth/{provider}/nonce, as put in the request (for Apple, before hashing)"`
		AuthorizationCode string   `json:"authorization_code,omitempty" maxLength:"4096" doc:"Apple only: exchanged for a refresh token revoked when the account is deleted"`
		Name              string   `json:"name,omitempty" maxLength:"200" doc:"Apple only: the name Apple gives the first time"`
		Password          string   `json:"password,omitempty" maxLength:"512" doc:"Required unless this session verified a second factor in the last 10 minutes; accounts without a password sign in again instead"`
	}
}

// removeIdentityInput's body is optional, like removePasskeyInput's.
type removeIdentityInput struct {
	ID   string `path:"id" maxLength:"64" example:"idn_nbswy3dpeb3w64tmmq"`
	Body *struct {
		_        struct{} `json:"-" additionalProperties:"true"`
		Password string   `json:"password,omitempty" maxLength:"512" doc:"Required unless this session verified a second factor in the last 10 minutes; accounts without a password sign in again instead"`
	}
}

type appleNotificationInput struct {
	Body struct {
		_       struct{} `json:"-" additionalProperties:"true"`
		Payload string   `json:"payload" maxLength:"16384"`
	}
}

// registerSocial adds the Google and Apple operations (ADR-0046).
func registerSocial(api huma.API, h *handler, public, signedIn func(huma.Operation) huma.Operation) {
	unavailable := []int{http.StatusServiceUnavailable}
	login := []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusServiceUnavailable}

	huma.Register(api, public(huma.Operation{
		OperationID: "auth-start-social", Method: http.MethodGet, Path: "/v1/auth/{provider}/start",
		Summary: "Sign in with Google or Apple in a browser",
		Description: "Open it in the browser (a link or redirect, not fetch): it sets a short-lived cookie and redirects to the provider. The provider returns to the callback, " +
			"which redirects to `return_to` signed in, or with `#mfa_challenge_token=…&methods=…` for a second factor, or `#error=<code>`.",
		DefaultStatus: http.StatusFound, Errors: []int{http.StatusUnprocessableEntity, http.StatusServiceUnavailable},
	}), h.startSocial)
	huma.Register(api, public(huma.Operation{
		OperationID: "auth-google-callback", Method: http.MethodGet, Path: "/v1/auth/google/callback",
		Summary: "Google's return to the API", Description: "Google redirects here; the API redirects to the sign-in's `return_to`.",
		DefaultStatus: http.StatusSeeOther,
	}), h.googleCallback)
	huma.Register(api, public(huma.Operation{
		OperationID: "auth-apple-callback", Method: http.MethodPost, Path: "/v1/auth/apple/callback",
		Summary: "Apple's return to the API", Description: "Apple posts the result here; the API redirects to the sign-in's `return_to`.",
		DefaultStatus: http.StatusSeeOther,
	}), h.appleCallback)
	huma.Register(api, public(huma.Operation{
		OperationID: "auth-social-nonce", Method: http.MethodPost, Path: "/v1/auth/{provider}/nonce",
		Summary:     "Get a nonce for a native sign-in",
		Description: "Put the nonce in Google's or Apple's SDK request, then send the ID token with it to `POST /v1/auth/{provider}/token`. It works once, for 5 minutes.",
		Errors:      unavailable,
	}), h.socialNonce)
	huma.Register(api, public(huma.Operation{
		OperationID: "auth-google-token", Method: http.MethodPost, Path: "/v1/auth/google/token",
		Summary:     "Sign in with a Google ID token",
		Description: "For iOS and Android apps. Returns a session like `POST /v1/auth/login`, or 202 with a second-factor challenge.",
		Errors:      login,
	}), h.googleToken)
	huma.Register(api, public(huma.Operation{
		OperationID: "auth-apple-token", Method: http.MethodPost, Path: "/v1/auth/apple/token",
		Summary:     "Sign in with an Apple ID token",
		Description: "For iOS apps. Returns a session like `POST /v1/auth/login`, or 202 with a second-factor challenge.",
		Errors:      login,
	}), h.appleToken)
	huma.Register(api, signedIn(huma.Operation{
		OperationID: "auth-list-identities", Method: http.MethodGet, Path: "/v1/auth/identities",
		Summary: "List linked Google and Apple accounts",
	}), h.listIdentities)
	huma.Register(api, signedIn(huma.Operation{
		OperationID: "auth-link-identity", Method: http.MethodPost, Path: "/v1/auth/identities",
		Summary: "Link a Google or Apple account",
		Description: "Links the provider account of an ID token to the signed-in user: get a nonce from `POST /v1/auth/{provider}/nonce`, put it in the SDK's " +
			"(or Google Identity Services' or Sign in with Apple JS's) request, then send the ID token here. Send the password unless this session verified " +
			"a second factor in the last 10 minutes. Signing in with a provider links an existing account by itself only when the provider manages the " +
			"address (Gmail, Google Workspace, iCloud, Apple relay); otherwise sign-in answers `social_link_required` and the user links here. " +
			"201 with the identity; 200 when it was already linked.",
		DefaultStatus: http.StatusCreated,
		Errors:        []int{http.StatusForbidden, http.StatusConflict, http.StatusTooManyRequests, http.StatusServiceUnavailable},
	}), h.linkIdentity)
	huma.Register(api, signedIn(huma.Operation{
		OperationID: "auth-remove-identity", Method: http.MethodDelete, Path: "/v1/auth/identities/{id}",
		Summary:       "Unlink a Google or Apple account",
		Description:   "Send the password unless this session verified a second factor in the last 10 minutes. Not allowed for the account's last way to sign in.",
		DefaultStatus: http.StatusNoContent, Errors: []int{http.StatusNotFound, http.StatusConflict},
	}), h.removeIdentity)
	huma.Register(api, public(huma.Operation{
		OperationID: "auth-apple-notifications", Method: http.MethodPost, Path: "/v1/auth/apple/notifications",
		Summary:     "Apple's server-to-server notifications",
		Description: "Register this URL in the App ID's Sign in with Apple settings. Apple posts consent and email changes signed with its keys.",
		Errors:      []int{http.StatusUnauthorized, http.StatusServiceUnavailable},
	}), h.appleNotification)
}

func (h *handler) startSocial(ctx context.Context, in *startSocialInput) (*redirectOutput, error) {
	st, err := h.svc.StartSocialSignIn(ctx, in.Provider, in.ReturnTo)
	if err != nil {
		return nil, authError(err)
	}
	//nolint:gosec // SameSite=None is required for Apple's cross-site post; the value is single use and bound to the state
	cookie := http.Cookie{
		Name: oauthCookie, Value: st.BrowserToken, Path: "/", Expires: st.ExpiresAt.UTC(), MaxAge: int(time.Until(st.ExpiresAt).Seconds()),
		// None: Apple posts the result from its own site.
		Secure: true, HttpOnly: true, SameSite: http.SameSiteNoneMode,
	}
	return &redirectOutput{Status: http.StatusFound, Location: st.URL, SetCookie: []http.Cookie{cookie}}, nil
}

func (h *handler) googleCallback(ctx context.Context, in *googleCallbackInput) (*redirectOutput, error) {
	return h.finishSocial(ctx, authdomain.ProviderGoogle, in.State, in.Browser, in.Code, in.Error, ""), nil
}

func (h *handler) appleCallback(ctx context.Context, in *appleCallbackInput) (*redirectOutput, error) {
	form, _ := url.ParseQuery(string(in.RawBody))
	return h.finishSocial(ctx, authdomain.ProviderApple, form.Get("state"), in.Browser, form.Get("code"), form.Get("error"), appleName(form.Get("user"))), nil
}

// finishSocial redirects the browser to the sign-in's return address: signed
// in, with a second-factor challenge, or with an error code, always in the
// fragment so tokens never reach servers or Referer headers.
func (h *handler) finishSocial(ctx context.Context, provider, state, browser, code, providerError, name string) *redirectOutput {
	if providerError != "" {
		code = "" // the person cancelled, or the provider refused: use up the state
	}
	res, err := h.svc.FinishSocialSignIn(ctx, provider, state, browser, code, name)
	clear := http.Cookie{Name: oauthCookie, Path: "/", MaxAge: -1, Secure: true, HttpOnly: true, SameSite: http.SameSiteNoneMode} //nolint:gosec // removes the SameSite=None cookie set above
	out := &redirectOutput{Status: http.StatusSeeOther, SetCookie: []http.Cookie{clear}}
	fragment := url.Values{}
	switch {
	case providerError != "" && !errors.Is(err, authdomain.ErrInvalidState):
		fragment.Set("error", "access_denied")
	case err != nil:
		fragment.Set("error", socialErrorCode(err))
	case res.Challenge != nil:
		fragment.Set("mfa_challenge_token", res.Challenge.Token)
		fragment.Set("methods", strings.Join(res.Challenge.Methods, ","))
		fragment.Set("expires_at", res.Challenge.ExpiresAt.UTC().Format(time.RFC3339))
	default:
		out.SetCookie = append(out.SetCookie, *authlib.SessionCookie(h.cookie, res.Token, res.Session.AbsoluteExpiresAt))
	}
	out.Location = res.ReturnTo
	if len(fragment) > 0 {
		out.Location += "#" + fragment.Encode()
	}
	return out
}

// socialErrorCode is the error code a failed web sign-in returns in the
// fragment; the same codes as the JSON errors.
func socialErrorCode(err error) string {
	for target, code := range map[error]string{
		authdomain.ErrInvalidState:          "invalid_state",
		authdomain.ErrInvalidSocialToken:    "invalid_social_token",
		authdomain.ErrSocialEmailUnverified: "social_email_unverified",
		authdomain.ErrSocialLinkRequired:    "social_link_required",
		authdomain.ErrSocialUnavailable:     "social_unavailable",
		authdomain.ErrMFAUnavailable:        "mfa_unavailable",
	} {
		if errors.Is(err, target) {
			return code
		}
	}
	return "server_error"
}

// appleName reads the name Apple posts once, as {"name":{"firstName":…,"lastName":…}}.
func appleName(user string) string {
	var u struct {
		Name struct {
			FirstName string `json:"firstName"`
			LastName  string `json:"lastName"`
		} `json:"name"`
	}
	if user == "" || json.Unmarshal([]byte(user), &u) != nil {
		return ""
	}
	return strings.TrimSpace(u.Name.FirstName + " " + u.Name.LastName)
}

func (h *handler) socialNonce(ctx context.Context, in *providerInput) (*socialNonceOutput, error) {
	nonce, expires, err := h.svc.SocialNonce(ctx, in.Provider)
	if err != nil {
		return nil, authError(err)
	}
	return &socialNonceOutput{Body: SocialNonceResponse{Nonce: nonce, ExpiresAt: expires}}, nil
}

func (h *handler) googleToken(ctx context.Context, in *googleTokenInput) (*loginOutput, error) {
	res, err := h.svc.SignInWithIDToken(ctx, authdomain.ProviderGoogle, in.Body.IDToken, in.Body.Nonce, "", "")
	if err != nil {
		return nil, authError(err)
	}
	return h.signedIn(res, in.Body.Transport), nil
}

func (h *handler) appleToken(ctx context.Context, in *appleTokenInput) (*loginOutput, error) {
	res, err := h.svc.SignInWithIDToken(ctx, authdomain.ProviderApple, in.Body.IDToken, in.Body.Nonce, in.Body.AuthorizationCode, in.Body.Name)
	if err != nil {
		return nil, authError(err)
	}
	return h.signedIn(res, in.Body.Transport), nil
}

func (h *handler) listIdentities(ctx context.Context, _ *struct{}) (*identityListOutput, error) {
	identities, err := h.svc.ListIdentities(ctx)
	if err != nil {
		return nil, err
	}
	out := &identityListOutput{Body: IdentityList{Identities: make([]IdentityResponse, len(identities))}}
	for i, id := range identities {
		out.Body.Identities[i] = identityResponse(id)
	}
	return out, nil
}

func identityResponse(id authdomain.Identity) IdentityResponse {
	return IdentityResponse{
		ID: id.ID, Provider: id.Provider, Email: id.Email, PrivateEmail: id.PrivateEmail, Name: id.Name, CreatedAt: id.CreatedAt, LastUsedAt: id.LastUsedAt,
	}
}

func (h *handler) linkIdentity(ctx context.Context, in *linkIdentityInput) (*identityOutput, error) {
	id, added, err := h.svc.LinkIdentity(ctx, in.Body.Provider, in.Body.IDToken, in.Body.Nonce, in.Body.AuthorizationCode, in.Body.Name, in.Body.Password)
	if err != nil {
		return nil, authError(err)
	}
	status := http.StatusCreated
	if !added {
		status = http.StatusOK
	}
	return &identityOutput{Status: status, Body: identityResponse(id)}, nil
}

func (h *handler) removeIdentity(ctx context.Context, in *removeIdentityInput) (*struct{}, error) {
	var password string
	if in.Body != nil {
		password = in.Body.Password
	}
	return nil, authError(h.svc.RemoveIdentity(ctx, in.ID, password))
}

func (h *handler) appleNotification(ctx context.Context, in *appleNotificationInput) (*struct{}, error) {
	return nil, authError(h.svc.HandleAppleNotification(ctx, in.Body.Payload))
}

// signedIn is the response for a sign-in: a session, or 202 with a
// second-factor challenge.
func (h *handler) signedIn(res authusecase.LoginResult, transport string) *loginOutput {
	if c := res.Challenge; c != nil {
		return &loginOutput{Status: http.StatusAccepted, Body: LoginResponse{
			MFA: &MFAChallenge{ChallengeToken: c.Token, Methods: c.Methods, ExpiresAt: c.ExpiresAt},
		}}
	}
	return h.session(res, transport)
}
