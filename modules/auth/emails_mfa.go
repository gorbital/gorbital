package auth

import (
	"context"
	"fmt"
)

func (e mailEmails) SendTwoFactorEnabled(ctx context.Context, to string) error {
	return e.send(ctx, to, "auth_mfa_enabled", "Two-factor authentication is on for "+e.app,
		fmt.Sprintf("Two-factor authentication was just turned on for your %s account, and other devices were signed out.", e.app),
		"Keep your recovery codes somewhere safe. If you didn't do this, reset your password and contact support right away.")
}

func (e mailEmails) SendTwoFactorDisabled(ctx context.Context, to string) error {
	return e.send(ctx, to, "auth_mfa_disabled", "Two-factor authentication is off for "+e.app,
		fmt.Sprintf("Two-factor authentication was just turned off for your %s account, and other devices were signed out.", e.app),
		"If you didn't do this, reset your password and turn two-factor authentication on again right away.")
}

func (e mailEmails) SendRecoveryCodeUsed(ctx context.Context, to string, remaining int) error {
	left := fmt.Sprintf("You have %d recovery codes left.", remaining)
	if remaining == 1 {
		left = "You have 1 recovery code left."
	}
	return e.send(ctx, to, "auth_recovery_code_used", "A recovery code was used for "+e.app,
		fmt.Sprintf("A recovery code was just used to sign in to your %s account.", e.app),
		left+" Create new ones from your account settings when you're running low.",
		"If you didn't do this, reset your password right away.")
}
