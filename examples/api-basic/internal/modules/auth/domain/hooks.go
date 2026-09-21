package domain

import (
	"errors"
	"regexp"
	"strings"
)

// Sign-in methods, as hooks and audit events name them (ADR-0083, Phase 6).
// Google, Apple and GitHub use their provider names; a module's own method
// uses the name it passes to SignIn.
const (
	MethodPassword = "password"
	MethodPasskey  = "passkey"
	// MethodOperator is an account an operator created (POST
	// /ops/auth/users, orb dev's seed data).
	MethodOperator = "operator"
)

// builtInMethods can't be a custom method's name.
var builtInMethods = []string{MethodPassword, MethodPasskey, MethodOperator, ProviderGoogle, ProviderApple, ProviderGitHub, "mfa", "api_key", "impersonation"}

var methodName = regexp.MustCompile(`^[a-z][a-z0-9_]{1,31}$`)

// ErrInvalidMethod reports a custom sign-in method name that isn't
// lowercase snake_case of 2 to 32 characters, or is a built-in method's.
var ErrInvalidMethod = errors.New("a custom sign-in method is named in lowercase snake_case, 2 to 32 characters, and isn't password, passkey, operator, google, apple, github, mfa, api_key or impersonation")

// CheckCustomMethod returns ErrInvalidMethod unless name can name a custom
// sign-in method.
func CheckCustomMethod(name string) error {
	if !methodName.MatchString(name) {
		return ErrInvalidMethod
	}
	for _, m := range builtInMethods {
		if name == m {
			return ErrInvalidMethod
		}
	}
	return nil
}

// challengeMethodSeparator joins a second-factor challenge's token and the
// sign-in method that started it. It isn't in the tokens' base64url
// alphabet.
const challengeMethodSeparator = "."

// ChallengeToken returns the token a client finishes a second-factor
// challenge with: token itself for a password sign-in, as in v0.1, or token
// and method joined, so LoginMFA knows how the sign-in started without a
// column. Only the hash of the whole string is stored, so a client can't
// change the method without the challenge becoming unknown.
func ChallengeToken(token, method string) string {
	if method == "" || method == MethodPassword {
		return token
	}
	return token + challengeMethodSeparator + method
}

// ChallengeMethod returns the sign-in method a challenge token names.
func ChallengeMethod(challengeToken string) string {
	if _, method, ok := strings.Cut(challengeToken, challengeMethodSeparator); ok {
		return method
	}
	return MethodPassword
}

// ErrRegistrationClosed reports a sign-up refused because the app creates
// accounts only through operators or its own flows (authhttp's
// WithoutRegistration): a first Google, Apple or GitHub sign-in of an
// address without an account.
var ErrRegistrationClosed = errors.New("registration is closed")

// Refusal is an app's hook refusing a sign-in or an account, with a
// problem code and detail the client receives with status 403.
type Refusal struct {
	Code   string
	Detail string
}

// Error returns the detail.
func (r *Refusal) Error() string { return "refused by the app: " + r.Code + ": " + r.Detail }

// HookError is an app's hook failing for another reason than a refusal.
// The sign-in or account creation fails closed, as a server error; the
// cause is logged, never sent.
type HookError struct {
	Hook string
	Err  error
}

func (e *HookError) Error() string { return "auth: " + e.Hook + " hook: " + e.Err.Error() }

// Unwrap returns the hook's error.
func (e *HookError) Unwrap() error { return e.Err }
