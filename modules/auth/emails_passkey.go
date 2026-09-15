package auth

import (
	"context"
	"fmt"
)

func (e mailEmails) SendPasskeyAdded(ctx context.Context, to, name string) error {
	return e.send(ctx, to, "auth_passkey_added", "A passkey was added to your "+e.app+" account",
		fmt.Sprintf("A passkey named %q was just added to your %s account. You can sign in with it instead of your password.", name, e.app),
		"If you didn't do this, remove it from your account settings and reset your password right away.")
}

func (e mailEmails) SendSignInMethodAdded(ctx context.Context, to, method string) error {
	return e.send(ctx, to, "auth_sign_in_method_added", method+" sign-in was added to your "+e.app+" account",
		fmt.Sprintf("Your %s account can now sign in with %s.", e.app, method),
		"If you didn't do this, remove it from your account settings and reset your password right away.")
}

func (e mailEmails) SendSignInMethodRemoved(ctx context.Context, to, method string) error {
	return e.send(ctx, to, "auth_sign_in_method_removed", method+" sign-in was removed from your "+e.app+" account",
		fmt.Sprintf("Your %s account can no longer sign in with %s.", e.app, method),
		"If you didn't do this, reset your password right away.")
}

func (e mailEmails) SendPasskeyRemoved(ctx context.Context, to, name string) error {
	return e.send(ctx, to, "auth_passkey_removed", "A passkey was removed from your "+e.app+" account",
		fmt.Sprintf("The passkey named %q was just removed from your %s account.", name, e.app),
		"If you didn't do this, reset your password right away.")
}
