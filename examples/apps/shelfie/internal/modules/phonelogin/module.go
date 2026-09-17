// Package phonelogin is Shelfie's phone-code sign-in: a reader confirms a
// phone number, then signs in with a code texted to it. It verifies the
// code and hands the reader to authhttp's SignIn, so the session is the one
// POST /v1/auth/login creates. Its Module takes the authenticator, so
// main.go adds it (orb gen modules lists only func Module()).
package phonelogin

import (
	"net/http"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/authhttp"
	"gorbital.dev/httpx"

	"example.com/shelfie/internal/modules/phonelogin/delivery"
	"example.com/shelfie/internal/modules/phonelogin/domain"
	"example.com/shelfie/internal/modules/phonelogin/repository"
	"example.com/shelfie/internal/modules/phonelogin/usecase"
)

// Sender sends codes in text messages.
type Sender = usecase.Sender

// docs:start module

// Module returns phone-code sign-in, signing readers in with auth, the
// authenticator main.go passes to gorbital.WithAuth, and texting codes
// with sender.
func Module(auth *authhttp.Authenticator, sender Sender) gorbital.Module {
	return gorbital.Module{
		Name: "phonelogin",
		Errors: []httpx.Mapping{
			{Err: domain.ErrUnauthenticated, Status: http.StatusUnauthorized, Code: "unauthenticated", Detail: "authentication is required"},
			{Err: domain.ErrInvalidPhone, Status: http.StatusUnprocessableEntity, Code: "invalid_phone", Detail: "a phone number is in E.164 form, such as +447700900123"},
			{Err: domain.ErrInvalidCode, Status: http.StatusUnauthorized, Code: "invalid_phone_code", Detail: "the code is wrong or expired; ask for a new one"},
			{Err: domain.ErrPhoneTaken, Status: http.StatusConflict, Code: "phone_taken", Detail: "another account uses this number"},
			{Err: domain.ErrSMSUnavailable, Status: http.StatusServiceUnavailable, Code: "sms_unavailable", Detail: "text messages can't be sent right now; try again later"},
		},
		Permissions: []gorbital.Permission{
			{Name: usecase.PermWrite, Description: "Set and confirm your phone number", Roles: []string{"user"}},
		},
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			delivery.Register(r, usecase.NewService(repository.NewStore(d.DB), sender), auth)
		},
	}
}

// docs:end module
