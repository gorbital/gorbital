package main

import (
	"os"

	"gorbital.dev/gorbital/authhttp"

	"example.com/shelfie/internal/modules/books"
	"example.com/shelfie/internal/modules/phonelogin"
	"example.com/shelfie/internal/modules/profiles"
)

// docs:start sign-in-options

// signInOptions are how Shelfie's sign-in differs from the library's
// defaults (chapter 6).
func signInOptions() []authhttp.Option {
	return []authhttp.Option{
		// POST /v1/auth/register also takes display_name and country, and
		// saves them in the transaction that creates the account.
		authhttp.RegisterFields(profiles.SaveRegistration),
		// Every new account starts with a shelf, however it was created.
		authhttp.OnRegister(books.CreateDefaultShelf),
		// Suspended readers can't sign in, whichever way they try.
		authhttp.BeforeLogin(profiles.RefuseSuspended),
		// Readers' passwords are at least 14 characters.
		authhttp.MinPasswordLength(14),
	}
}

// docs:end sign-in-options

// docs:start sms-sender

// smsSender texts phone sign-in codes (chapter 7). Shelfie has no SMS
// provider yet: in development codes go to the log, and production answers
// 503 sms_unavailable until a real sender replaces this one.
func smsSender() phonelogin.Sender {
	return phonelogin.LogSender{Production: os.Getenv("APP_ENV") == "production"}
}

// docs:end sms-sender
