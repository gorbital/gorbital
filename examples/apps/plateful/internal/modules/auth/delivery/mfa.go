package delivery

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	authdomain "example.com/plateful/internal/modules/auth/domain"
)

// TOTPEnrollmentResponse is a new authenticator app secret.
type TOTPEnrollmentResponse struct {
	Secret string `json:"secret" doc:"Type it into an authenticator app. It is shown once." example:"JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP"`
	URI    string `json:"uri" doc:"otpauth:// URI for authenticator apps. It is shown once."`
	QRCode string `json:"qr_code" doc:"The URI as a PNG QR code data URL, ready for an <img> tag. It is shown once."`
}

// RecoveryCodesResponse is a new set of recovery codes.
type RecoveryCodesResponse struct {
	RecoveryCodes []string `json:"recovery_codes" doc:"Single-use codes for signing in without the authenticator app or passkeys. They are shown once; keep them somewhere safe."`
}

// PasskeyFactor is a passkey's response given as a second factor.
type PasskeyFactor struct {
	CeremonyToken string         `json:"ceremony_token" maxLength:"256" doc:"From POST /v1/auth/login/mfa/passkey when signing in, or POST /v1/auth/passkeys/verification when signed in"`
	Credential    map[string]any `json:"credential" doc:"The PublicKeyCredential from navigator.credentials.get(), as JSON"`
}

type totpEnrollmentOutput struct{ Body TOTPEnrollmentResponse }

type recoveryCodesOutput struct{ Body RecoveryCodesResponse }

type loginMFAInput struct {
	Body struct {
		_              struct{}       `json:"-" additionalProperties:"true"`
		ChallengeToken string         `json:"challenge_token" maxLength:"256" doc:"From the 202 response of POST /v1/auth/login"`
		Code           string         `json:"code,omitempty" maxLength:"16" example:"123456" doc:"A code from the authenticator app"`
		RecoveryCode   string         `json:"recovery_code,omitempty" maxLength:"32" example:"abcd-efgh-ijkl-mnop" doc:"A recovery code, instead of code"`
		Passkey        *PasskeyFactor `json:"passkey,omitempty" doc:"A passkey's response, instead of code"`
		Transport      string         `json:"transport,omitempty" enum:"cookie,bearer" default:"cookie" doc:"cookie (browsers): an HttpOnly session cookie; bearer (native apps): the token in the response"`
	}
}

type passwordInput struct {
	Body struct {
		_        struct{} `json:"-" additionalProperties:"true"`
		Password string   `json:"password" maxLength:"512"`
	}
}

type totpCodeInput struct {
	Body struct {
		_    struct{} `json:"-" additionalProperties:"true"`
		Code string   `json:"code,omitempty" maxLength:"16" example:"123456" doc:"A code from the authenticator app"`
	}
}

type regenerateRecoveryCodesInput struct {
	Body struct {
		_       struct{}       `json:"-" additionalProperties:"true"`
		Code    string         `json:"code,omitempty" maxLength:"16" example:"123456" doc:"A code from the authenticator app"`
		Passkey *PasskeyFactor `json:"passkey,omitempty" doc:"A passkey's response, instead of code"`
	}
}

type disableTOTPInput struct {
	Body struct {
		_            struct{}       `json:"-" additionalProperties:"true"`
		Password     string         `json:"password" maxLength:"512"`
		Code         string         `json:"code,omitempty" maxLength:"16" example:"123456" doc:"A code from the authenticator app"`
		RecoveryCode string         `json:"recovery_code,omitempty" maxLength:"32" example:"abcd-efgh-ijkl-mnop" doc:"A recovery code, instead of code"`
		Passkey      *PasskeyFactor `json:"passkey,omitempty" doc:"A passkey's response, instead of code"`
	}
}

// registerMFA adds the two-factor authentication operations (ADR-0043).
func registerMFA(r *routes, h *handler, public, signedIn func(huma.Operation) huma.Operation) {
	unavailable := []int{http.StatusConflict, http.StatusTooManyRequests, http.StatusServiceUnavailable}

	route(r, public(huma.Operation{
		OperationID: "auth-login-mfa", Method: http.MethodPost, Path: "/v1/auth/login/mfa",
		Summary: "Finish signing in with a second factor",
		Description: "After a 202 from `POST /v1/auth/login`: send the challenge token with a code from the authenticator app, a recovery code, or a passkey's response " +
			"(start it with `POST /v1/auth/login/mfa/passkey`). Each sign-in allows 5 attempts within 5 minutes; each code works once.",
		Errors: []int{http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusServiceUnavailable},
	}), h.loginMFA)
	route(r, signedIn(huma.Operation{
		OperationID: "auth-start-totp", Method: http.MethodPost, Path: "/v1/auth/mfa/totp",
		Summary:     "Start setting up an authenticator app",
		Description: "Requires the password. Returns a secret, an otpauth:// URI and a QR code image, shown once. It turns on when a code is confirmed with `POST /v1/auth/mfa/totp/confirm`.",
		Errors:      unavailable,
	}), h.startTOTP)
	route(r, signedIn(huma.Operation{
		OperationID: "auth-confirm-totp", Method: http.MethodPost, Path: "/v1/auth/mfa/totp/confirm",
		Summary:     "Turn on the authenticator app",
		Description: "Send a code from the authenticator app. Returns 10 recovery codes, shown once, verifies this session with a second factor and signs out other devices.",
		Errors:      unavailable,
	}), h.confirmTOTP)
	route(r, signedIn(huma.Operation{
		OperationID: "auth-disable-totp", Method: http.MethodDelete, Path: "/v1/auth/mfa/totp",
		Summary: "Turn off the authenticator app",
		Description: "Requires the password and a code, recovery code or passkey response (start one with `POST /v1/auth/passkeys/verification`); signs out other devices. " +
			"Not allowed while a role requires two-factor authentication and no passkey is left.",
		DefaultStatus: http.StatusNoContent, Errors: unavailable,
	}), h.disableTOTP)
	route(r, signedIn(huma.Operation{
		OperationID: "auth-regenerate-recovery-codes", Method: http.MethodPost, Path: "/v1/auth/mfa/recovery-codes",
		Summary:     "Replace the recovery codes",
		Description: "Send a code from the authenticator app or a passkey's response (start one with `POST /v1/auth/passkeys/verification`). Returns 10 new codes, shown once; the old codes stop working.",
		Errors:      unavailable,
	}), h.regenerateRecoveryCodes)
}

// secondFactor is the second factor a request body carries.
func secondFactor(code, recoveryCode string, pk *PasskeyFactor) (authdomain.SecondFactor, error) {
	factor := authdomain.SecondFactor{Code: code, RecoveryCode: recoveryCode}
	if pk != nil {
		credential, err := json.Marshal(pk.Credential)
		if err != nil {
			return factor, err
		}
		factor.Passkey = &authdomain.PasskeyAssertion{CeremonyToken: pk.CeremonyToken, Credential: credential}
	}
	return factor, nil
}

func (h *handler) loginMFA(ctx context.Context, in *loginMFAInput) (*LoginOutput, error) {
	factor, err := secondFactor(in.Body.Code, in.Body.RecoveryCode, in.Body.Passkey)
	if err != nil {
		return nil, err
	}
	res, err := h.svc.LoginMFA(ctx, in.Body.ChallengeToken, factor)
	if err != nil {
		return nil, authError(err)
	}
	return h.session(res, in.Body.Transport), nil
}

func (h *handler) startTOTP(ctx context.Context, in *passwordInput) (*totpEnrollmentOutput, error) {
	e, err := h.svc.StartTOTPEnrollment(ctx, in.Body.Password)
	if err != nil {
		return nil, authError(err)
	}
	return &totpEnrollmentOutput{Body: TOTPEnrollmentResponse{Secret: e.Secret, URI: e.URI, QRCode: e.QRCode}}, nil
}

func (h *handler) confirmTOTP(ctx context.Context, in *totpCodeInput) (*recoveryCodesOutput, error) {
	codes, err := h.svc.ConfirmTOTP(ctx, in.Body.Code)
	if err != nil {
		return nil, authError(err)
	}
	return &recoveryCodesOutput{Body: RecoveryCodesResponse{RecoveryCodes: codes}}, nil
}

func (h *handler) disableTOTP(ctx context.Context, in *disableTOTPInput) (*struct{}, error) {
	factor, err := secondFactor(in.Body.Code, in.Body.RecoveryCode, in.Body.Passkey)
	if err != nil {
		return nil, err
	}
	return nil, authError(h.svc.DisableTOTP(ctx, in.Body.Password, factor))
}

func (h *handler) regenerateRecoveryCodes(ctx context.Context, in *regenerateRecoveryCodesInput) (*recoveryCodesOutput, error) {
	factor, err := secondFactor(in.Body.Code, "", in.Body.Passkey)
	if err != nil {
		return nil, err
	}
	codes, err := h.svc.RegenerateRecoveryCodes(ctx, factor)
	if err != nil {
		return nil, authError(err)
	}
	return &recoveryCodesOutput{Body: RecoveryCodesResponse{RecoveryCodes: codes}}, nil
}
