package authhttp

import (
	"gorbital.dev/gorbital"
	"gorbital.dev/modules/auth/social"

	"example.com/shelfie/internal/modules/auth/delivery/signintest"
)

// mountSignInTests serves the Dev Portal's sign-in tests when the dev
// console is on (ADR-0087): their endpoints under /_dev/auth/test/ behind
// the console's checks, the passkey ceremony page when passkeys are on, and
// the interception of test round trips on the callbacks (routes). They use
// the providers, relying party and keys sign-in uses, and nothing they do
// reaches the database.
func (a *Authenticator) mountSignInTests(s gorbital.AuthSetup, google, apple, gitHub *social.Provider) {
	if !s.DevConsole || s.DevEndpoints == nil || s.Handle == nil {
		return
	}
	t := signintest.New(signintest.Config{
		AppName: s.Name, App: s.Config,
		Google: google, Apple: apple, GitHub: gitHub,
		GoogleEndpoints: a.endpoints.Google, AppleEndpoints: a.endpoints.Apple, GitHubEndpoints: a.endpoints.GitHub,
		Passkeys: passkeys(s.Name, s.Config),
		Keyring:  keyring(s.Config),
		Logger:   s.Deps.Logger.With("source", "auth"),
	})
	s.DevEndpoints(signintest.ConsolePrefix, t.ConsoleHandler())
	if passkeys(s.Name, s.Config) != nil {
		page := t.PasskeyHandler()
		s.Handle("GET "+signintest.PasskeyPagePath, page)
		s.Handle("POST "+signintest.PasskeyPagePath+"/options", page)
		s.Handle("POST "+signintest.PasskeyPagePath+"/finish", page)
	}
	a.tester = t
}

// signInTester returns the sign-in tests, or nil when the dev console is
// off.
func (a *Authenticator) signInTester() *signintest.Tester {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.tester
}
