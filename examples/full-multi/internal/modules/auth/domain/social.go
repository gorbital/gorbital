package domain

import (
	"errors"
	"time"
)

// Sign-in providers (ADR-0046, ADR-0059).
const (
	ProviderGoogle = "google"
	ProviderApple  = "apple"
	ProviderGitHub = "github"
)

const (
	// OAuthStateTTL is how long a web sign-in can wait for the provider.
	OAuthStateTTL = 10 * time.Minute
	// SocialNonceTTL is how long a native app's nonce can be used.
	SocialNonceTTL = 5 * time.Minute
)

// Errors returned by the Google, Apple and GitHub sign-in use cases.
var (
	// ErrSocialUnavailable reports a provider, or a flow of one, that isn't
	// configured on this server.
	ErrSocialUnavailable = errors.New("this sign-in provider isn't configured")
	// ErrInvalidSocialToken reports an ID token, code or nonce that fails
	// verification, without saying which.
	ErrInvalidSocialToken = errors.New("the sign-in with the provider couldn't be verified")
	// ErrInvalidState reports an unknown, used or expired web sign-in, or one
	// started in another browser.
	ErrInvalidState = errors.New("the sign-in expired or was started in another browser")
	// ErrSocialEmailUnverified reports a new identity whose provider hasn't
	// verified the email address.
	ErrSocialEmailUnverified = errors.New("the provider hasn't verified this email address")
	// ErrInvalidReturnTo reports a return address outside the allowed origins.
	ErrInvalidReturnTo = errors.New("return_to isn't on an allowed origin")
	// ErrIdentityNotFound reports an identity that doesn't exist or isn't the
	// user's.
	ErrIdentityNotFound = errors.New("identity not found")
	// ErrLastSignInMethod reports removing an account's only way to sign in.
	ErrLastSignInMethod = errors.New("this is the account's last way to sign in")
	// ErrIdentityTaken reports an identity linked at the same moment by
	// another request.
	ErrIdentityTaken = errors.New("identity already linked")
	// ErrSocialLinkRequired reports a new identity whose email address has an
	// account, from a provider that isn't authoritative for the address: the
	// account's owner signs in and links it with LinkIdentity (security
	// review AUTH-M-1).
	ErrSocialLinkRequired = errors.New("an account with this email address exists; sign in and link the provider from the account")
	// ErrIdentityInUse reports linking an identity that is linked to another
	// account.
	ErrIdentityInUse = errors.New("this provider account is linked to another account")
)

// Identity is a Google, Apple or GitHub account linked to a user.
type Identity struct {
	ID       string
	UserID   string
	Provider string
	// Subject is the provider's stable ID of the person.
	Subject      string
	Email        string
	PrivateEmail bool
	Name         string
	// RefreshKeyID, RefreshTokenCiphertext and RefreshClientID hold Apple's
	// refresh token, encrypted, for revocation; they never leave the use
	// cases.
	RefreshKeyID           string
	RefreshTokenCiphertext []byte
	RefreshClientID        string
	CreatedAt              time.Time
	LastUsedAt             *time.Time
}

// OAuthState is a web sign-in waiting for the provider's answer. Only the
// hashes of its state and browser values are stored.
type OAuthState struct {
	ID          string
	TokenHash   []byte
	BrowserHash []byte
	Provider    string
	Nonce       string
	Verifier    string
	ReturnTo    string
	// LinkUserID and LinkSessionID are set when a signed-in user started the
	// flow to link the provider to their account (ADR-0059): the callback
	// links to that account, only while that session is active, and signs
	// no one in.
	LinkUserID    string
	LinkSessionID string
	ExpiresAt     time.Time
	ConsumedAt    *time.Time
	CreatedAt     time.Time
}

// UsableAt reports whether the sign-in can still finish at now.
func (s OAuthState) UsableAt(now time.Time) bool {
	return s.ConsumedAt == nil && now.Before(s.ExpiresAt)
}

// SocialNonce is a nonce given to a native app. Only its hash is stored.
type SocialNonce struct {
	ID        string
	TokenHash []byte
	Provider  string
	ExpiresAt time.Time
	CreatedAt time.Time
}
