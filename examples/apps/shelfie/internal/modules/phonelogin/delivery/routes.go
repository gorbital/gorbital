// Package delivery is phone-code sign-in's HTTP adapter: the route table in
// this file, and one file per operation.
package delivery

import (
	"net/http"
	"time"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"

	authhttp "example.com/shelfie/internal/modules/auth"
	"example.com/shelfie/internal/modules/phonelogin/usecase"
)

type handlers struct {
	svc  *usecase.Service
	auth *authhttp.Authenticator
}

// docs:start routes

// Register adds phone-code sign-in's routes to r. auth signs readers in
// once their code is verified.
func Register(r *gorbital.Router, svc *usecase.Service, auth *authhttp.Authenticator) {
	h := handlers{svc: svc, auth: auth}

	phone := r.Group("/v1/phone", gorbital.Tags("Phone sign-in"))
	gorbital.Put(phone, "", h.setPhone,
		gorbital.Summary("Set your phone number"), gorbital.Description("Texts a code to confirm it with POST /v1/phone/confirm."),
		gorbital.Status(http.StatusAccepted), guard.Permission(usecase.PermWrite), guard.RateLimit(5, time.Hour))
	gorbital.Post(phone, "/confirm", h.confirmPhone,
		gorbital.Summary("Confirm your phone number"), gorbital.Status(http.StatusNoContent),
		guard.Permission(usecase.PermWrite), guard.RateLimit(10, time.Hour))

	signIn := r.Group("/v1/phone-sign-in", gorbital.Tags("Phone sign-in"), guard.Public())
	gorbital.Post(signIn, "/code", h.sendSignInCode,
		gorbital.Summary("Text a sign-in code"),
		gorbital.Description("The response is the same whether or not an account confirmed the number."),
		gorbital.Status(http.StatusAccepted), gorbital.Errors(http.StatusServiceUnavailable),
		guard.RateLimit(10, time.Hour, guard.ByIP()))
	gorbital.Post(signIn, "", h.signIn,
		gorbital.Summary("Sign in with a texted code"),
		gorbital.Description("Answers as POST /v1/auth/login does: 200 with the session, or 202 with a second-factor challenge to finish with POST /v1/auth/login/mfa."),
		gorbital.Errors(http.StatusUnauthorized, http.StatusForbidden, http.StatusServiceUnavailable),
		guard.RateLimit(20, time.Hour, guard.ByIP()))
}

// docs:end routes
