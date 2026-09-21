package delivery

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	authdomain "example.com/shelfie/internal/modules/auth/domain"
	authusecase "example.com/shelfie/internal/modules/auth/usecase"
)

// PasskeyResponse is a passkey of the signed-in user.
type PasskeyResponse struct {
	ID         string     `json:"id" example:"pky_nbswy3dpeb3w64tmmq"`
	Name       string     `json:"name" example:"MacBook"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	BackedUp   bool       `json:"backed_up" doc:"Synced to the user's other devices by their passkey provider"`
}

// PasskeyCeremonyResponse starts a passkey ceremony in the client.
type PasskeyCeremonyResponse struct {
	CeremonyToken string    `json:"ceremony_token" doc:"Send it back with the passkey's response. It is shown once and expires after 5 minutes."`
	Options       any       `json:"options" doc:"Pass to navigator.credentials.create() or navigator.credentials.get() (PublicKeyCredential.parseCreationOptionsFromJSON or parseRequestOptionsFromJSON reads them)"`
	ExpiresAt     time.Time `json:"expires_at"`
}

// PasskeyCreatedResponse is a new passkey.
type PasskeyCreatedResponse struct {
	Passkey       PasskeyResponse `json:"passkey"`
	RecoveryCodes []string        `json:"recovery_codes,omitempty" doc:"When this passkey is the account's first second factor: 10 single-use codes, shown once"`
}

// PasskeyList is the signed-in user's passkeys, oldest first.
type PasskeyList struct {
	Passkeys []PasskeyResponse `json:"passkeys"`
}

type passkeyCeremonyOutput struct{ Body PasskeyCeremonyResponse }

type passkeyCreatedOutput struct{ Body PasskeyCreatedResponse }

type passkeyListOutput struct{ Body PasskeyList }

type beginPasskeyRegistrationInput struct {
	Body struct {
		_        struct{} `json:"-" additionalProperties:"true"`
		Password string   `json:"password,omitempty" maxLength:"512" doc:"Required unless this session verified a second factor in the last 10 minutes"`
	}
}

type finishPasskeyRegistrationInput struct {
	Body struct {
		_             struct{}       `json:"-" additionalProperties:"true"`
		CeremonyToken string         `json:"ceremony_token" maxLength:"256"`
		Name          string         `json:"name,omitempty" maxLength:"100" example:"MacBook" doc:"A label to recognise it by; default Passkey"`
		Credential    map[string]any `json:"credential" doc:"The PublicKeyCredential from navigator.credentials.create(), as JSON (PublicKeyCredential.toJSON())"`
	}
}

type renamePasskeyInput struct {
	ID   string `path:"id" maxLength:"64" example:"pky_nbswy3dpeb3w64tmmq"`
	Body struct {
		_    struct{} `json:"-" additionalProperties:"true"`
		Name string   `json:"name" maxLength:"100" example:"Work laptop"`
	}
}

// removePasskeyInput's body is optional: a session that verified a second
// factor in the last 10 minutes sends none.
type removePasskeyInput struct {
	ID   string `path:"id" maxLength:"64" example:"pky_nbswy3dpeb3w64tmmq"`
	Body *struct {
		_        struct{} `json:"-" additionalProperties:"true"`
		Password string   `json:"password,omitempty" maxLength:"512" doc:"Required unless this session verified a second factor in the last 10 minutes"`
	}
}

type passkeyLoginInput struct {
	Body struct {
		_             struct{}       `json:"-" additionalProperties:"true"`
		CeremonyToken string         `json:"ceremony_token" maxLength:"256" doc:"From POST /v1/auth/passkeys/login/options"`
		Credential    map[string]any `json:"credential" doc:"The PublicKeyCredential from navigator.credentials.get(), as JSON"`
		Transport     string         `json:"transport,omitempty" enum:"cookie,bearer" default:"cookie" doc:"cookie (browsers): an HttpOnly session cookie; bearer (native apps): the token in the response"`
	}
}

type passkeySecondFactorInput struct {
	Body struct {
		_              struct{} `json:"-" additionalProperties:"true"`
		ChallengeToken string   `json:"challenge_token" maxLength:"256" doc:"From the 202 response of POST /v1/auth/login"`
	}
}

// registerPasskeys adds the passkey operations (ADR-0044).
func registerPasskeys(r *routes, h *handler, public, signedIn func(huma.Operation) huma.Operation) {
	unavailable := []int{http.StatusServiceUnavailable}

	route(r, signedIn(huma.Operation{
		OperationID: "auth-begin-passkey-registration", Method: http.MethodPost, Path: "/v1/auth/passkeys/registration",
		Summary: "Start adding a passkey",
		Description: "Returns the options for `navigator.credentials.create()`. Send the password unless this session verified a second factor in the last 10 minutes; " +
			"once the account has two-factor authentication on, the session must also have verified one.",
		Errors: []int{http.StatusConflict, http.StatusServiceUnavailable},
	}), h.beginPasskeyRegistration)
	route(r, signedIn(huma.Operation{
		OperationID: "auth-create-passkey", Method: http.MethodPost, Path: "/v1/auth/passkeys",
		Summary:       "Add a passkey",
		Description:   "Send the browser's response to the registration options. Verifies this session with a second factor; the account's first second factor also returns recovery codes.",
		DefaultStatus: http.StatusCreated, Errors: []int{http.StatusConflict, http.StatusUnprocessableEntity, http.StatusServiceUnavailable},
	}), h.createPasskey)
	route(r, signedIn(huma.Operation{
		OperationID: "auth-list-passkeys", Method: http.MethodGet, Path: "/v1/auth/passkeys",
		Summary: "List passkeys",
	}), h.listPasskeys)
	route(r, signedIn(huma.Operation{
		OperationID: "auth-rename-passkey", Method: http.MethodPatch, Path: "/v1/auth/passkeys/{id}",
		Summary: "Rename a passkey", DefaultStatus: http.StatusNoContent, Errors: []int{http.StatusNotFound, http.StatusUnprocessableEntity},
	}), h.renamePasskey)
	route(r, signedIn(huma.Operation{
		OperationID: "auth-remove-passkey", Method: http.MethodDelete, Path: "/v1/auth/passkeys/{id}",
		Summary:       "Remove a passkey",
		Description:   "Send the password unless this session verified a second factor in the last 10 minutes. Not allowed for the last second factor while a role requires two-factor authentication.",
		DefaultStatus: http.StatusNoContent, Errors: []int{http.StatusNotFound, http.StatusConflict},
	}), h.removePasskey)
	route(r, signedIn(huma.Operation{
		OperationID: "auth-begin-passkey-verification", Method: http.MethodPost, Path: "/v1/auth/passkeys/verification",
		Summary: "Confirm a change with a passkey",
		Description: "Returns options for `navigator.credentials.get()` limited to the user's passkeys. Send the response as `passkey` when deleting the account, " +
			"turning off the authenticator app or replacing recovery codes.",
		Errors: []int{http.StatusConflict, http.StatusServiceUnavailable},
	}), h.beginPasskeyVerification)
	route(r, public(huma.Operation{
		OperationID: "auth-passkey-login-options", Method: http.MethodPost, Path: "/v1/auth/passkeys/login/options",
		Summary:     "Start signing in with a passkey",
		Description: "Returns the options for `navigator.credentials.get()`. No email or password: the chosen passkey names the account.",
		Errors:      unavailable,
	}), h.passkeyLoginOptions)
	route(r, public(huma.Operation{
		OperationID: "auth-passkey-login", Method: http.MethodPost, Path: "/v1/auth/passkeys/login",
		Summary:     "Sign in with a passkey",
		Description: "Send the passkey's response. Starts a session verified with a second factor, like `POST /v1/auth/login/mfa`.",
		Errors:      []int{http.StatusUnauthorized, http.StatusServiceUnavailable},
	}), h.passkeyLogin)
	route(r, public(huma.Operation{
		OperationID: "auth-login-mfa-passkey", Method: http.MethodPost, Path: "/v1/auth/login/mfa/passkey",
		Summary:     "Start a passkey second factor",
		Description: "After a 202 from `POST /v1/auth/login` listing `passkey`: returns options limited to the account's passkeys. Send the response to `POST /v1/auth/login/mfa`.",
		Errors:      []int{http.StatusUnauthorized, http.StatusServiceUnavailable},
	}), h.passkeySecondFactor)
}

func (h *handler) beginPasskeyRegistration(ctx context.Context, in *beginPasskeyRegistrationInput) (*passkeyCeremonyOutput, error) {
	c, err := h.svc.BeginPasskeyRegistration(ctx, in.Body.Password)
	if err != nil {
		return nil, authError(err)
	}
	return ceremonyOutput(c), nil
}

func (h *handler) createPasskey(ctx context.Context, in *finishPasskeyRegistrationInput) (*passkeyCreatedOutput, error) {
	credential, err := json.Marshal(in.Body.Credential)
	if err != nil {
		return nil, err
	}
	reg, err := h.svc.FinishPasskeyRegistration(ctx, in.Body.CeremonyToken, in.Body.Name, credential)
	if err != nil {
		return nil, authError(err)
	}
	return &passkeyCreatedOutput{Body: PasskeyCreatedResponse{Passkey: passkeyResponse(reg.Passkey), RecoveryCodes: reg.RecoveryCodes}}, nil
}

func (h *handler) listPasskeys(ctx context.Context, _ *struct{}) (*passkeyListOutput, error) {
	pks, err := h.svc.ListPasskeys(ctx)
	if err != nil {
		return nil, err
	}
	out := &passkeyListOutput{Body: PasskeyList{Passkeys: make([]PasskeyResponse, len(pks))}}
	for i, pk := range pks {
		out.Body.Passkeys[i] = passkeyResponse(pk)
	}
	return out, nil
}

func (h *handler) renamePasskey(ctx context.Context, in *renamePasskeyInput) (*struct{}, error) {
	return nil, authError(h.svc.RenamePasskey(ctx, in.ID, in.Body.Name))
}

func (h *handler) removePasskey(ctx context.Context, in *removePasskeyInput) (*struct{}, error) {
	var password string
	if in.Body != nil {
		password = in.Body.Password
	}
	return nil, authError(h.svc.RemovePasskey(ctx, in.ID, password))
}

func (h *handler) beginPasskeyVerification(ctx context.Context, _ *struct{}) (*passkeyCeremonyOutput, error) {
	c, err := h.svc.BeginPasskeyVerification(ctx)
	if err != nil {
		return nil, authError(err)
	}
	return ceremonyOutput(c), nil
}

func (h *handler) passkeyLoginOptions(ctx context.Context, _ *struct{}) (*passkeyCeremonyOutput, error) {
	c, err := h.svc.BeginPasskeyLogin(ctx)
	if err != nil {
		return nil, authError(err)
	}
	return ceremonyOutput(c), nil
}

func (h *handler) passkeyLogin(ctx context.Context, in *passkeyLoginInput) (*LoginOutput, error) {
	credential, err := json.Marshal(in.Body.Credential)
	if err != nil {
		return nil, err
	}
	res, err := h.svc.FinishPasskeyLogin(ctx, in.Body.CeremonyToken, credential)
	if err != nil {
		return nil, authError(err)
	}
	return h.session(res, in.Body.Transport), nil
}

func (h *handler) passkeySecondFactor(ctx context.Context, in *passkeySecondFactorInput) (*passkeyCeremonyOutput, error) {
	c, err := h.svc.BeginPasskeySecondFactor(ctx, in.Body.ChallengeToken)
	if err != nil {
		return nil, authError(err)
	}
	return ceremonyOutput(c), nil
}

func ceremonyOutput(c authusecase.PasskeyCeremony) *passkeyCeremonyOutput {
	return &passkeyCeremonyOutput{Body: PasskeyCeremonyResponse{CeremonyToken: c.Token, Options: c.Options, ExpiresAt: c.ExpiresAt}}
}

func passkeyResponse(pk authdomain.Passkey) PasskeyResponse {
	return PasskeyResponse{ID: pk.ID, Name: pk.Name, CreatedAt: pk.CreatedAt, LastUsedAt: pk.LastUsedAt, BackedUp: pk.BackupState}
}
