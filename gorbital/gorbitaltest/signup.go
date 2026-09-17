package gorbitaltest

import (
	"net/http"
	"regexp"
	"testing"
)

// SignUpPassword is the password of the accounts [App.SignUp] creates.
const SignUpPassword = "correct horse battery staple"

// sixDigits finds a verification code in an email.
var sixDigits = regexp.MustCompile(`\b(\d{6})\b`)

// SignUp creates an account for email through sign-in's own endpoints, in
// an app with gorbital.dev/gorbital/authhttp passed to gorbital.WithAuth:
// it registers with [SignUpPassword], verifies the address with the code
// from the queued email, and signs in with a bearer token. It returns a
// client whose requests carry the token, and the account's user ID.
//
// The account is real: hooks run (such as the personal workspace of
// gorbital.dev/gorbital/orgshttp), and its requests go through the
// authenticator, API key scopes and second factors included, so tests of
// organisations and other features that store the user's ID use it rather
// than [User].
func (a *App) SignUp(t testing.TB, email string) (*Client, string) {
	t.Helper()
	anon := a.Client()
	anon.Post("/v1/auth/register", map[string]string{"email": email, "password": SignUpPassword}).AssertStatus(t, http.StatusAccepted)
	code := ""
	sent := a.Mail(t)
	for i := len(sent) - 1; i >= 0 && code == ""; i-- {
		if len(sent[i].To) > 0 && sent[i].To[0].Email == email {
			if m := sixDigits.FindStringSubmatch(sent[i].Text); m != nil {
				code = m[1]
			}
		}
	}
	if code == "" {
		t.Fatalf("gorbitaltest: SignUp(%s): no verification email was queued; does the app use authhttp?", email)
	}
	anon.Post("/v1/auth/verify-email", map[string]string{"email": email, "code": code}).AssertStatus(t, http.StatusNoContent)

	res := anon.Post("/v1/auth/login", map[string]string{"email": email, "password": SignUpPassword, "transport": "bearer"})
	res.AssertStatus(t, http.StatusOK)
	var login struct {
		Token string `json:"token"`
	}
	res.JSON(t, &login)
	if login.Token == "" {
		t.Fatalf("gorbitaltest: SignUp(%s): login returned no token: %s", email, res.Body)
	}
	client := a.Client().WithHeader("Authorization", "Bearer "+login.Token)
	var me struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	res = client.Get("/v1/auth/me")
	res.AssertStatus(t, http.StatusOK)
	res.JSON(t, &me)
	return client, me.User.ID
}
