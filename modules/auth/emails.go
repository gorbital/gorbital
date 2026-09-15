package auth

import (
	"context"
	"fmt"
	"html"
	"time"

	"apistock.dev/mail"
)

// Emails sends the emails authentication needs. Apps use [NewMailEmails] or
// their own templates. Implementations should queue rather than deliver
// inline (jobs.AsyncSender), so requests don't wait for the provider.
type Emails interface {
	// SendVerificationCode sends the code that verifies to's address.
	SendVerificationCode(ctx context.Context, to, code string, ttl time.Duration) error
	// SendPasswordResetCode sends the code that resets to's password.
	SendPasswordResetCode(ctx context.Context, to, code string, ttl time.Duration) error
	// SendAccountExists tells to that someone tried to register with an
	// address that already has an account.
	SendAccountExists(ctx context.Context, to string) error
	// SendPasswordChanged tells to that their password changed.
	SendPasswordChanged(ctx context.Context, to string) error
	// SendTwoFactorEnabled tells to that two-factor authentication was
	// turned on.
	SendTwoFactorEnabled(ctx context.Context, to string) error
	// SendTwoFactorDisabled tells to that two-factor authentication was
	// turned off.
	SendTwoFactorDisabled(ctx context.Context, to string) error
	// SendRecoveryCodeUsed tells to that a recovery code was used to sign in
	// and how many are left.
	SendRecoveryCodeUsed(ctx context.Context, to string, remaining int) error
	// SendSignInMethodAdded tells to that a sign-in method such as Google was
	// linked to their account.
	SendSignInMethodAdded(ctx context.Context, to, method string) error
	// SendSignInMethodRemoved tells to that a sign-in method was unlinked.
	SendSignInMethodRemoved(ctx context.Context, to, method string) error
	// SendPasskeyAdded tells to that a passkey named name was added.
	SendPasskeyAdded(ctx context.Context, to, name string) error
	// SendPasskeyRemoved tells to that a passkey named name was removed.
	SendPasskeyRemoved(ctx context.Context, to, name string) error
}

type mailEmails struct {
	sender mail.Sender
	app    string
}

// NewMailEmails returns plain, readable emails sent through sender, which
// sets the From address (mail.WithDefaults). appName appears in subjects and
// bodies.
func NewMailEmails(sender mail.Sender, appName string) Emails {
	return mailEmails{sender: sender, app: appName}
}

func (e mailEmails) send(ctx context.Context, to, category, subject string, paragraphs ...string) error {
	text, htmlBody := "", ""
	for _, p := range paragraphs {
		text += p + "\n\n"
		htmlBody += "<p>" + html.EscapeString(p) + "</p>\n"
	}
	return e.sender.Send(ctx, mail.Message{
		To:      []mail.Address{{Email: to}},
		Subject: subject,
		Text:    text,
		HTML:    htmlBody,
		Tags:    map[string]string{"category": category},
	})
}

func (e mailEmails) SendVerificationCode(ctx context.Context, to, code string, ttl time.Duration) error {
	return e.send(ctx, to, "auth_verification", "Verify your email for "+e.app,
		fmt.Sprintf("Your %s verification code is %s", e.app, code),
		fmt.Sprintf("It expires in %s.", humanDuration(ttl)),
		"If you didn't create an account, you can ignore this email.")
}

func (e mailEmails) SendPasswordResetCode(ctx context.Context, to, code string, ttl time.Duration) error {
	return e.send(ctx, to, "auth_password_reset", "Reset your "+e.app+" password",
		fmt.Sprintf("Your %s password reset code is %s", e.app, code),
		fmt.Sprintf("It expires in %s.", humanDuration(ttl)),
		"If you didn't ask to reset your password, you can ignore this email; your password hasn't changed.")
}

func (e mailEmails) SendAccountExists(ctx context.Context, to string) error {
	return e.send(ctx, to, "auth_account_exists", "Your "+e.app+" account",
		fmt.Sprintf("Someone tried to create a %s account with this email address, which already has one.", e.app),
		"If it was you, sign in instead, or reset your password if you've forgotten it. If it wasn't, you can ignore this email.")
}

func (e mailEmails) SendPasswordChanged(ctx context.Context, to string) error {
	return e.send(ctx, to, "auth_password_changed", "Your "+e.app+" password was changed",
		fmt.Sprintf("The password for your %s account was just changed, and other devices were signed out.", e.app),
		"If you didn't do this, reset your password right away.")
}

func humanDuration(d time.Duration) string {
	switch {
	case d >= time.Hour && d%time.Hour == 0:
		if d == time.Hour {
			return "1 hour"
		}
		return fmt.Sprintf("%d hours", d/time.Hour)
	case d >= time.Minute:
		if d/time.Minute == 1 {
			return "1 minute"
		}
		return fmt.Sprintf("%d minutes", d/time.Minute)
	default:
		return d.String()
	}
}
