package delivery

import (
	"context"

	authhttp "example.com/shelfie/internal/modules/auth"
	"example.com/shelfie/internal/modules/phonelogin/domain"
)

// docs:start sign-in

type signInInput struct {
	Body struct {
		Phone     string `json:"phone" maxLength:"32" example:"+447700900123"`
		Code      string `json:"code" pattern:"^[0-9]{6}$" example:"123456"`
		Transport string `json:"transport,omitempty" enum:"cookie,bearer" default:"cookie" doc:"cookie (browsers): an HttpOnly session cookie; bearer (native apps): the token in the response"`
	}
}

// signIn verifies the texted code, then hands the reader to sign-in, which
// answers exactly as POST /v1/auth/login does: login limits, bans, a second
// factor, BeforeLogin, the audit event and the session's transport.
func (h handlers) signIn(ctx context.Context, in *signInInput) (*authhttp.SignedIn, error) {
	userID, err := h.svc.VerifySignInCode(ctx, in.Body.Phone, in.Body.Code)
	if err != nil {
		return nil, err
	}
	return h.auth.SignIn(ctx, authhttp.SignInRequest{UserID: userID, Method: domain.Method, Transport: in.Body.Transport})
}

// docs:end sign-in
