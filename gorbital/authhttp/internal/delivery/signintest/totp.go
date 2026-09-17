package signintest

import (
	"fmt"
	"strings"
	"time"

	authlib "gorbital.dev/modules/auth"
)

// totpDriftSteps is how far either side of now a code is looked for when it
// doesn't verify, to tell a wrong code from a wrong clock: 10 minutes.
const totpDriftSteps = 20

// totpTest is a throwaway authenticator app secret.
type totpTest struct {
	secret    string
	attempts  int
	expiresAt time.Time
}

// TOTPStart is POST /_dev/auth/test/totp/start.
type TOTPStart struct {
	ID        string    `json:"id"`
	Secret    string    `json:"secret"`
	URI       string    `json:"uri"`
	QRCode    string    `json:"qr_code"`
	Issuer    string    `json:"issuer"`
	Account   string    `json:"account"`
	ExpiresAt time.Time `json:"expires_at"`
	Checks    []Check   `json:"checks"`
}

// TOTPResult is POST /_dev/auth/test/totp/verify.
type TOTPResult struct {
	Passed       bool   `json:"passed"`
	Code         string `json:"code"`
	Message      string `json:"message"`
	Fix          string `json:"fix,omitempty"`
	DriftSteps   int64  `json:"drift_steps"`
	DriftSeconds int64  `json:"drift_seconds"`
	AttemptsLeft int    `json:"attempts_left"`
}

// StartTOTP creates a throwaway secret for an authenticator app, never
// stored anywhere but this process's memory, with the same parameters and
// QR code enrolment uses. It returns ErrTooManyTests.
func (t *Tester) StartTOTP() (TOTPStart, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sweep()
	if len(t.totps) >= MaxPending {
		return TOTPStart{}, ErrTooManyTests
	}
	now := t.cfg.Now()
	issuer, account := t.cfg.AppName+" test", "sign-in test"
	if t.cfg.AppName == "" {
		issuer = "gorbital test"
	}
	s := &totpTest{secret: authlib.NewTOTPSecret(), expiresAt: now.Add(TOTPTTL)}
	uri := authlib.TOTPURI(issuer, account, s.secret)
	qr, err := authlib.TOTPQRCode(uri)
	if err != nil {
		return TOTPStart{}, err
	}
	id := authlib.NewID("tot")
	t.totps[id] = s
	return TOTPStart{
		ID: id, Secret: s.secret, URI: uri, QRCode: qr, Issuer: issuer, Account: account, ExpiresAt: s.expiresAt,
		Checks: []Check{t.keyringCheck()},
	}, nil
}

// VerifyTOTP checks code against test id's secret as sign-in does (one step
// of 30 seconds either side), and when it doesn't verify, looks further to
// report a clock that drifted. A passed test, an expired one or one with
// MaxTOTPAttempts wrong codes is forgotten. It returns ErrTestNotFound.
func (t *Tester) VerifyTOTP(id, code string) (TOTPResult, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s, ok := t.totps[id]
	if !ok {
		return TOTPResult{}, ErrTestNotFound
	}
	now := t.cfg.Now()
	if !now.Before(s.expiresAt) {
		delete(t.totps, id)
		return TOTPResult{Code: "expired", Message: "the test's secret expired after " + TOTPTTL.String(), Fix: "start again and remove the old entry from the authenticator app"}, nil
	}
	s.attempts++
	left := MaxTOTPAttempts - s.attempts
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	current := authlib.TOTPStep(now)
	if step, ok := authlib.VerifyTOTP(s.secret, code, now); ok {
		delete(t.totps, id)
		drift := step - current
		msg := "the code verifies: the authenticator app and this server agree"
		if drift != 0 {
			msg = fmt.Sprintf("the code verifies, one step (%d seconds) %s, within what sign-in accepts", int64(authlib.TOTPPeriod/time.Second), earlierLater(drift))
		}
		return TOTPResult{Passed: true, Code: "ok", Message: msg, DriftSteps: drift, DriftSeconds: drift * int64(authlib.TOTPPeriod/time.Second), AttemptsLeft: left}, nil
	}
	res := TOTPResult{Code: "invalid_code", Message: "the code doesn't match this test's secret at any time within 10 minutes",
		Fix: "scan this test's QR code (not an older entry) and type the code the app shows now", AttemptsLeft: left}
	if len(code) == authlib.TOTPDigits {
		for d := int64(1); d <= totpDriftSteps && res.Code == "invalid_code"; d++ {
			for _, step := range []int64{current - d, current + d} {
				if want, err := authlib.TOTPCode(s.secret, time.Unix(step*int64(authlib.TOTPPeriod/time.Second), 0)); err == nil && want == code {
					drift := step - current
					res = TOTPResult{Code: "clock_drift", DriftSteps: drift, DriftSeconds: drift * int64(authlib.TOTPPeriod/time.Second), AttemptsLeft: left,
						Message: fmt.Sprintf("the code is right for %d seconds %s: the phone's clock and this computer's disagree, and sign-in accepts only %d seconds either side",
							abs(drift)*int64(authlib.TOTPPeriod/time.Second), earlierLater(drift), int64(authlib.TOTPPeriod/time.Second)),
						Fix: "turn on automatic date and time on the phone and on this computer"}
					break
				}
			}
		}
	}
	if left <= 0 {
		delete(t.totps, id)
		if res.Code == "invalid_code" {
			res.Code, res.Fix = "too_many_attempts", "start a new test"
		}
	}
	return res, nil
}

func earlierLater(drift int64) string {
	if drift < 0 {
		return "earlier"
	}
	return "later"
}

func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}
