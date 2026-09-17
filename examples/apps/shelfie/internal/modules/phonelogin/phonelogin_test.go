package phonelogin_test

import (
	"context"
	"net/http"
	"regexp"
	"sync"
	"testing"
	"time"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/authhttp"
	"gorbital.dev/gorbital/gorbitaltest"
	authlib "gorbital.dev/modules/auth"

	"example.com/shelfie/db/migrations"
	"example.com/shelfie/internal/modules/phonelogin"
)

// docs:start fake-sms

// fakeSMS records the codes it would text, instead of calling a provider.
type fakeSMS struct {
	mu   sync.Mutex
	sent map[string][]string // phone → codes, oldest first
}

func (f *fakeSMS) SendCode(_ context.Context, phone, code string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sent == nil {
		f.sent = map[string][]string{}
	}
	f.sent[phone] = append(f.sent[phone], code)
	return nil
}

// count returns how many codes were texted to phone.
func (f *fakeSMS) count(phone string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent[phone])
}

// last returns the newest code texted to phone, or "".
func (f *fakeSMS) last(phone string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if codes := f.sent[phone]; len(codes) > 0 {
		return codes[len(codes)-1]
	}
	return ""
}

// newApp builds Shelfie's sign-in with phone-code sign-in texting through
// sms, as main.go wires them.
func newApp(t *testing.T, sms *fakeSMS) *gorbitaltest.App {
	t.Helper()
	auth := authhttp.New()
	return gorbitaltest.NewWithEnv(t, map[string]string{"AUTH_ENCRYPTION_KEYS": authlib.NewKeyringKey("k1")},
		gorbital.WithAuth(auth),
		gorbital.WithModules(phonelogin.Module(auth, sms)),
		gorbital.WithMigrations(migrations.FS),
	)
}

// docs:end fake-sms

const password = "a long enough password"

var sixDigits = regexp.MustCompile(`\b(\d{6})\b`)

// signUp registers and verifies an account, and returns a client signed in
// with a bearer token.
func signUp(t *testing.T, app *gorbitaltest.App, email string) *gorbitaltest.Client {
	t.Helper()
	anon := app.Client()
	anon.Post("/v1/auth/register", map[string]string{"email": email, "password": password}).AssertStatus(t, http.StatusAccepted)
	var code string
	for _, m := range app.Mail(t) {
		if len(m.To) > 0 && m.To[0].Email == email {
			if found := sixDigits.FindStringSubmatch(m.Text); found != nil {
				code = found[1]
			}
		}
	}
	anon.Post("/v1/auth/verify-email", map[string]string{"email": email, "code": code}).AssertStatus(t, http.StatusNoContent)
	return bearer(t, app, anon.Post("/v1/auth/login", map[string]string{"email": email, "password": password, "transport": "bearer"}))
}

// bearer returns a client with the session token of a 200 sign-in.
func bearer(t *testing.T, app *gorbitaltest.App, res *gorbitaltest.Response) *gorbitaltest.Client {
	t.Helper()
	res.AssertStatus(t, http.StatusOK)
	var session struct {
		Token string `json:"token"`
	}
	res.JSON(t, &session)
	return app.Client().WithHeader("Authorization", "Bearer "+session.Token)
}

// docs:start phone-sign-in

func TestPhoneSignIn(t *testing.T) {
	sms := &fakeSMS{}
	app := newApp(t, sms)
	ada := signUp(t, app, "ada@example.com")
	const phone = "+447700900123"

	// Ada adds her number and confirms it with the texted code.
	ada.Put("/v1/phone", map[string]string{"phone": "+44 7700 900123"}).AssertStatus(t, http.StatusAccepted)
	ada.Post("/v1/phone/confirm", map[string]string{"phone": phone, "code": sms.last(phone)}).AssertStatus(t, http.StatusNoContent)

	// Later, on a new phone, she asks for a sign-in code. A number nobody
	// confirmed gets the same answer, and no text.
	visitor := app.Client()
	visitor.Post("/v1/phone-sign-in/code", map[string]string{"phone": phone}).AssertStatus(t, http.StatusAccepted)
	visitor.Post("/v1/phone-sign-in/code", map[string]string{"phone": "+15555550100"}).AssertStatus(t, http.StatusAccepted)
	if sms.last("+15555550100") != "" {
		t.Error("a code was texted to a number no account confirmed")
	}

	code := sms.last(phone)
	visitor.Post("/v1/phone-sign-in", map[string]string{"phone": phone, "code": wrong(code)}).
		AssertProblem(t, http.StatusUnauthorized, "invalid_phone_code")

	// The right code signs her in, as POST /v1/auth/login would.
	res := visitor.Post("/v1/phone-sign-in", map[string]string{"phone": phone, "code": code, "transport": "bearer"})
	signedIn := bearer(t, app, res)
	var me struct {
		User struct {
			Email string `json:"email"`
		} `json:"user"`
	}
	signedIn.Get("/v1/auth/me").JSON(t, &me)
	if me.User.Email != "ada@example.com" {
		t.Errorf("GET /v1/auth/me = %+v", me)
	}

	// A code works once.
	visitor.Post("/v1/phone-sign-in", map[string]string{"phone": phone, "code": code}).
		AssertProblem(t, http.StatusUnauthorized, "invalid_phone_code")
}

// docs:end phone-sign-in

// docs:start phone-sign-in-mfa

// TestPhoneSignInKeepsTheSecondFactor: with an authenticator app on, a phone
// code is only the first factor: 202, then POST /v1/auth/login/mfa.
func TestPhoneSignInKeepsTheSecondFactor(t *testing.T) {
	sms := &fakeSMS{}
	app := newApp(t, sms)
	ada := signUp(t, app, "ada@example.com")
	const phone = "+447700900123"
	ada.Put("/v1/phone", map[string]string{"phone": phone}).AssertStatus(t, http.StatusAccepted)
	ada.Post("/v1/phone/confirm", map[string]string{"phone": phone, "code": sms.last(phone)}).AssertStatus(t, http.StatusNoContent)

	var totp struct {
		Secret string `json:"secret"`
	}
	ada.Post("/v1/auth/mfa/totp", map[string]string{"password": password}).JSON(t, &totp)
	totpCode, _ := authlib.TOTPCode(totp.Secret, time.Now())
	var recovery struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	ada.Post("/v1/auth/mfa/totp/confirm", map[string]string{"code": totpCode}).JSON(t, &recovery)

	visitor := app.Client()
	visitor.Post("/v1/phone-sign-in/code", map[string]string{"phone": phone}).AssertStatus(t, http.StatusAccepted)
	res := visitor.Post("/v1/phone-sign-in", map[string]string{"phone": phone, "code": sms.last(phone), "transport": "bearer"})
	res.AssertStatus(t, http.StatusAccepted)
	var challenge struct {
		Token string `json:"token"`
		MFA   struct {
			ChallengeToken string   `json:"challenge_token"`
			Methods        []string `json:"methods"`
		} `json:"mfa"`
	}
	res.JSON(t, &challenge)
	if challenge.Token != "" || challenge.MFA.ChallengeToken == "" {
		t.Fatalf("phone sign-in with 2FA = %+v, want a challenge and no session", challenge)
	}
	bearer(t, app, visitor.Post("/v1/auth/login/mfa", map[string]string{
		"challenge_token": challenge.MFA.ChallengeToken, "recovery_code": recovery.RecoveryCodes[0], "transport": "bearer",
	}))
}

// docs:end phone-sign-in-mfa

// TestPhoneCodesAreBounded: a number receives at most one code a minute, and
// a code allows five guesses.
func TestPhoneCodesAreBounded(t *testing.T) {
	sms := &fakeSMS{}
	app := newApp(t, sms)
	ada := signUp(t, app, "ada@example.com")
	const phone = "+447700900123"
	ada.Put("/v1/phone", map[string]string{"phone": phone}).AssertStatus(t, http.StatusAccepted)
	ada.Post("/v1/phone/confirm", map[string]string{"phone": phone, "code": sms.last(phone)}).AssertStatus(t, http.StatusNoContent)

	visitor := app.Client()
	visitor.Post("/v1/phone-sign-in/code", map[string]string{"phone": phone}).AssertStatus(t, http.StatusAccepted)
	visitor.Post("/v1/phone-sign-in/code", map[string]string{"phone": phone}).AssertStatus(t, http.StatusAccepted)
	if n := sms.count(phone); n != 2 { // the confirmation code and one sign-in code
		t.Errorf("%d texts, want the second request within a minute ignored", n)
	}
	code := sms.last(phone)
	for range 5 {
		visitor.Post("/v1/phone-sign-in", map[string]string{"phone": phone, "code": wrong(code)}).
			AssertProblem(t, http.StatusUnauthorized, "invalid_phone_code")
	}
	visitor.Post("/v1/phone-sign-in", map[string]string{"phone": phone, "code": code}).
		AssertProblem(t, http.StatusUnauthorized, "invalid_phone_code")
	visitor.Post("/v1/phone-sign-in/code", map[string]string{"phone": "not a number"}).
		AssertProblem(t, http.StatusUnprocessableEntity, "invalid_phone")
}

// wrong returns another 6-digit code than code.
func wrong(code string) string {
	if code == "000000" {
		return "000001"
	}
	return "000000"
}
