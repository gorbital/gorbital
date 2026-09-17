package domain

import (
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Limits for API keys and service accounts (ADR-0058).
const (
	// MaxAPIKeys is how many usable (not revoked or expired) API keys one
	// user or service account can have.
	MaxAPIKeys = 20
	// MaxAPIKeyScopes is how many permissions one API key can be limited to.
	MaxAPIKeyScopes = 50
	// MaxServiceAccounts is how many service accounts the platform, or one
	// organisation, can have.
	MaxServiceAccounts = 100
	// MaxServiceAccountRoles is how many platform roles one service account
	// can hold; an organisation's service account holds exactly one role.
	MaxServiceAccountRoles = 10
	// MinAPIKeyLifetime is the shortest lifetime of a new API key.
	MinAPIKeyLifetime = time.Hour
)

// Errors returned by the API key and service account use cases.
var (
	// ErrSessionRequired reports a request authenticated with an API key to
	// an operation that needs a signed-in session: an API key can't change
	// the account's sign-in methods or create more keys.
	ErrSessionRequired = errors.New("this needs a signed-in session, not an API key")
	// ErrForbidden reports a signed-in user without the permission.
	ErrForbidden = errors.New("missing permission")
	// ErrStepUpRequired reports a permission of a role that requires
	// two-factor authentication, held by a session without it.
	ErrStepUpRequired = errors.New("the permission needs a session signed in with two-factor authentication")
	// ErrAPIKeyNotFound reports an API key that doesn't exist or isn't the
	// owner's.
	ErrAPIKeyNotFound = errors.New("API key not found")
	// ErrInvalidAPIKeyName reports a name that is empty, longer than 100
	// characters or has control characters.
	ErrInvalidAPIKeyName = errors.New("API key name must be 1 to 100 characters on one line")
	// ErrInvalidAPIKeyExpiry reports an expiry in less than MinAPIKeyLifetime
	// or beyond auth.api_key_max_ttl.
	ErrInvalidAPIKeyExpiry = errors.New("API key expiry is out of range")
	// ErrInvalidAPIKeyScopes reports a scope that isn't a permission the
	// key's owner holds without two-factor authentication, or too many.
	ErrInvalidAPIKeyScopes = errors.New("API key scopes must be permissions the owner holds")
	// ErrAPIKeyLimitReached reports an owner with MaxAPIKeys usable keys.
	ErrAPIKeyLimitReached = errors.New("too many API keys")
	// ErrServiceAccountNotFound reports a service account that doesn't exist
	// or belongs to another organisation, or to the platform.
	ErrServiceAccountNotFound = errors.New("service account not found")
	// ErrInvalidServiceAccount reports a name that isn't 1 to 100 characters
	// on one line, or a description longer than 500 characters.
	ErrInvalidServiceAccount = errors.New("service account name must be 1 to 100 characters on one line, and its description at most 500")
	// ErrInvalidServiceAccountRole reports a role that isn't declared, that
	// requires two-factor authentication, that is an organisation's owner
	// role or above the caller's own, or the wrong number of roles.
	ErrInvalidServiceAccountRole = errors.New("service accounts can't hold this role")
	// ErrServiceAccountLimitReached reports MaxServiceAccounts service
	// accounts.
	ErrServiceAccountLimitReached = errors.New("too many service accounts")
	// ErrServiceAccountDisabled reports creating a key for a disabled
	// service account.
	ErrServiceAccountDisabled = errors.New("service account is disabled")
)

// Reasons an API key was revoked, stored with it and recorded in audit
// events.
const (
	RevokedByOwner         = "revoked_by_owner"
	RevokedByAdmin         = "revoked_by_admin"
	RevokedAccountDisabled = "service_account_disabled"
	RevokedAccountDeleted  = "account_deleted"
	RevokedPasswordReset   = "password_reset"
	RevokedAddressClaimed  = "address_claimed"
)

// API key statuses.
const (
	APIKeyStatusActive  = "active"
	APIKeyStatusExpired = "expired"
	APIKeyStatusRevoked = "revoked"
)

// API key owner types.
const (
	OwnerUser           = "user"
	OwnerServiceAccount = "service_account"
)

// ServiceAccount is a non-human principal with roles: of the platform, or
// of one organisation.
type ServiceAccount struct {
	ID string
	// OrgID is the organisation the service account belongs to; empty for a
	// platform service account.
	OrgID       string
	Name        string
	Description string
	// Roles are platform roles, or the one organisation role.
	Roles      []string
	CreatedBy  string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	DisabledAt *time.Time
}

// Disabled reports whether the service account was disabled.
func (a ServiceAccount) Disabled() bool { return a.DisabledAt != nil }

// APIKey is a key of a user or a service account. Only the hash of the key
// is stored.
type APIKey struct {
	ID string
	// LookupID is the middle part of the key: it finds the key and may be
	// logged.
	LookupID string
	// SecretHash is the SHA-256 of the whole key. It never leaves the use
	// cases.
	SecretHash []byte
	// Exactly one of UserID and ServiceAccountID is set.
	UserID           string
	ServiceAccountID string
	Name             string
	// Scopes limit the key to these permissions; empty means its owner's.
	Scopes        []string
	ExpiresAt     time.Time
	CreatedBy     string
	CreatedAt     time.Time
	LastUsedAt    *time.Time
	RevokedAt     *time.Time
	RevokedReason string
}

// ActiveAt reports whether the key can be used at now.
func (k APIKey) ActiveAt(now time.Time) bool {
	return k.RevokedAt == nil && now.Before(k.ExpiresAt)
}

// StatusAt is active, expired or revoked.
func (k APIKey) StatusAt(now time.Time) string {
	switch {
	case k.RevokedAt != nil:
		return APIKeyStatusRevoked
	case !now.Before(k.ExpiresAt):
		return APIKeyStatusExpired
	default:
		return APIKeyStatusActive
	}
}

// OwnerType is user or service_account.
func (k APIKey) OwnerType() string {
	if k.ServiceAccountID != "" {
		return OwnerServiceAccount
	}
	return OwnerUser
}

// OwnerID is the user or service account ID.
func (k APIKey) OwnerID() string {
	if k.ServiceAccountID != "" {
		return k.ServiceAccountID
	}
	return k.UserID
}

// APIKeyName returns a trimmed API key name, or ErrInvalidAPIKeyName.
func APIKeyName(name string) (string, error) {
	name, ok := oneLine(name, 100)
	if !ok {
		return "", ErrInvalidAPIKeyName
	}
	return name, nil
}

// ServiceAccountFields returns a trimmed name and description, or
// ErrInvalidServiceAccount.
func ServiceAccountFields(name, description string) (string, string, error) {
	name, ok := oneLine(name, 100)
	description = strings.TrimSpace(description)
	if !ok || utf8.RuneCountInString(description) > 500 || !utf8.ValidString(description) || strings.ContainsRune(description, 0) {
		return "", "", ErrInvalidServiceAccount
	}
	return name, description, nil
}

// oneLine trims s and reports whether it has 1 to limit characters and no
// control characters.
func oneLine(s string, limit int) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" || !utf8.ValidString(s) || utf8.RuneCountInString(s) > limit || strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return "", false
	}
	return s, true
}

var permissionName = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`)

// APIKeyScopes returns scopes sorted without duplicates, or
// ErrInvalidAPIKeyScopes for a name that can't be a permission or too many.
// Whether the owner holds them is checked by the use cases.
func APIKeyScopes(scopes []string) ([]string, error) {
	out := slices.Clone(scopes)
	slices.Sort(out)
	out = slices.Compact(out)
	if len(out) > MaxAPIKeyScopes {
		return nil, ErrInvalidAPIKeyScopes
	}
	for _, s := range out {
		if len(s) > 200 || !permissionName.MatchString(s) {
			return nil, ErrInvalidAPIKeyScopes
		}
	}
	if out == nil {
		out = []string{}
	}
	return out, nil
}

// Roles returns roles sorted without duplicates.
func Roles(roles []string) []string {
	out := slices.Clone(roles)
	slices.Sort(out)
	out = slices.Compact(out)
	if out == nil {
		out = []string{}
	}
	return out
}
