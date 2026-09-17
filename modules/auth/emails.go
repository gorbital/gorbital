package auth

import (
	"context"
	"fmt"
	"time"

	"gorbital.dev/mail"
)

// Emails sends the emails authentication needs. Apps use [NewBrandedEmails]
// or their own templates. Implementations should queue rather than deliver
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
	brand  mail.Brand
}

// NewMailEmails returns the emails of [NewBrandedEmails] with appName as the
// whole brand: no link, logo or support address.
func NewMailEmails(sender mail.Sender, appName string) Emails {
	return NewBrandedEmails(sender, mail.Brand{Name: appName})
}

// NewBrandedEmails returns emails in brand's layout ([mail.Brand.Render])
// sent through sender, which sets the From address (mail.WithDefaults).
// brand.Name appears in subjects and bodies.
func NewBrandedEmails(sender mail.Sender, brand mail.Brand) Emails {
	return mailEmails{sender: sender, brand: brand}
}

// app is the name used in subjects and sentences.
func (e mailEmails) app() string { return e.brand.Name }

func (e mailEmails) send(ctx context.Context, to, category, subject string, email mail.Email) error {
	return e.sender.Send(ctx, e.brand.Message(to, subject, category, email))
}

// didntDoThis is the closing line of every notice about a change someone
// else could have made.
const didntDoThis = "If you didn't do this, reset your password right away."

func (e mailEmails) SendVerificationCode(ctx context.Context, to, code string, ttl time.Duration) error {
	return e.send(ctx, to, "auth_verification", "Verify your email for "+e.app(), mail.Email{
		Preheader:  fmt.Sprintf("Your %s verification code is %s", e.app(), code),
		Title:      "Verify your email",
		Paragraphs: []string{fmt.Sprintf("Enter this code to confirm your email address for %s.", e.app())},
		Code:       code,
		CodeLabel:  "Verification code",
		CodeNote:   fmt.Sprintf("It expires in %s.", humanDuration(ttl)),
		Closing:    []string{fmt.Sprintf("If you didn't create a %s account, you can ignore this email.", e.app())},
	})
}

func (e mailEmails) SendPasswordResetCode(ctx context.Context, to, code string, ttl time.Duration) error {
	return e.send(ctx, to, "auth_password_reset", "Reset your "+e.app()+" password", mail.Email{
		Preheader:  fmt.Sprintf("Your %s password reset code is %s", e.app(), code),
		Title:      "Reset your password",
		Paragraphs: []string{fmt.Sprintf("Enter this code to choose a new password for your %s account.", e.app())},
		Code:       code,
		CodeLabel:  "Password reset code",
		CodeNote:   fmt.Sprintf("It expires in %s.", humanDuration(ttl)),
		Closing:    []string{"If you didn't ask to reset your password, you can ignore this email. Your password hasn't changed."},
	})
}

func (e mailEmails) SendAccountExists(ctx context.Context, to string) error {
	return e.send(ctx, to, "auth_account_exists", "Your "+e.app()+" account", mail.Email{
		Preheader:  "This email address already has an account",
		Title:      "You already have an account",
		Paragraphs: []string{fmt.Sprintf("Someone tried to create a %s account with this email address, which already has one.", e.app())},
		Closing: []string{
			"If it was you, sign in instead, or reset your password if you've forgotten it.",
			"If it wasn't, you can ignore this email.",
		},
	})
}

func (e mailEmails) SendPasswordChanged(ctx context.Context, to string) error {
	return e.send(ctx, to, "auth_password_changed", "Your "+e.app()+" password was changed", mail.Email{
		Preheader:  "Your password was just changed",
		Title:      "Your password was changed",
		Paragraphs: []string{fmt.Sprintf("The password for your %s account was just changed, and other devices were signed out.", e.app())},
		Closing:    []string{didntDoThis},
	})
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

// EmailPreview is one of the emails this module sends, rendered with
// sample data for the Dev Portal's template preview (ADR-0074).
type EmailPreview struct {
	// Name is auth.<kind>, such as auth.verification_code.
	Name        string
	Description string
	Category    string
	// Build renders the message for to.
	Build func(ctx context.Context, to string) (mail.Message, error)
}

// EmailPreviews returns the previews of [BrandedEmailPreviews] with appName
// as the whole brand.
func EmailPreviews(appName string) []EmailPreview {
	return BrandedEmailPreviews(mail.Brand{Name: appName})
}

// BrandedEmailPreviews returns a preview of every email [NewBrandedEmails]
// sends, with sample codes and names, in brand's layout. Category is the
// message's own tag.
func BrandedEmailPreviews(brand mail.Brand) []EmailPreview {
	previews := emailPreviews(brand)
	for i := range previews {
		if m, err := previews[i].Build(context.Background(), "preview@example.com"); err == nil {
			previews[i].Category = m.Tags["category"]
		}
	}
	return previews
}

func emailPreviews(brand mail.Brand) []EmailPreview {
	build := func(fn func(e Emails, ctx context.Context, to string) error) func(context.Context, string) (mail.Message, error) {
		return func(ctx context.Context, to string) (mail.Message, error) {
			var captured mail.Message
			e := NewBrandedEmails(mail.SenderFunc(func(_ context.Context, m mail.Message) error {
				captured = m
				return nil
			}), brand)
			if err := fn(e, ctx, to); err != nil {
				return mail.Message{}, err
			}
			return captured, nil
		}
	}
	return []EmailPreview{
		{Name: "auth.verification_code", Description: "After registering, or when an address changes: the code that verifies it",
			Build: build(func(e Emails, ctx context.Context, to string) error {
				return e.SendVerificationCode(ctx, to, "483920", 15*time.Minute)
			})},
		{Name: "auth.password_reset_code", Description: "After POST /v1/auth/password/forgot: the code that resets the password",
			Build: build(func(e Emails, ctx context.Context, to string) error {
				return e.SendPasswordResetCode(ctx, to, "176204", 15*time.Minute)
			})},
		{Name: "auth.account_exists", Description: "When someone registers with an address that already has an account",
			Build: build(func(e Emails, ctx context.Context, to string) error { return e.SendAccountExists(ctx, to) })},
		{Name: "auth.password_changed", Description: "After a password change or reset",
			Build: build(func(e Emails, ctx context.Context, to string) error { return e.SendPasswordChanged(ctx, to) })},
		{Name: "auth.two_factor_enabled", Description: "When two-factor authentication is turned on",
			Build: build(func(e Emails, ctx context.Context, to string) error { return e.SendTwoFactorEnabled(ctx, to) })},
		{Name: "auth.two_factor_disabled", Description: "When two-factor authentication is turned off",
			Build: build(func(e Emails, ctx context.Context, to string) error { return e.SendTwoFactorDisabled(ctx, to) })},
		{Name: "auth.recovery_code_used", Description: "When a recovery code signs someone in",
			Build: build(func(e Emails, ctx context.Context, to string) error { return e.SendRecoveryCodeUsed(ctx, to, 7) })},
		{Name: "auth.sign_in_method_added", Description: "When a provider such as Google is linked",
			Build: build(func(e Emails, ctx context.Context, to string) error {
				return e.SendSignInMethodAdded(ctx, to, "Google")
			})},
		{Name: "auth.sign_in_method_removed", Description: "When a provider is unlinked",
			Build: build(func(e Emails, ctx context.Context, to string) error {
				return e.SendSignInMethodRemoved(ctx, to, "Google")
			})},
		{Name: "auth.passkey_added", Description: "When a passkey is added",
			Build: build(func(e Emails, ctx context.Context, to string) error {
				return e.SendPasskeyAdded(ctx, to, "MacBook Pro")
			})},
		{Name: "auth.passkey_removed", Description: "When a passkey is removed",
			Build: build(func(e Emails, ctx context.Context, to string) error {
				return e.SendPasskeyRemoved(ctx, to, "MacBook Pro")
			})},
	}
}
