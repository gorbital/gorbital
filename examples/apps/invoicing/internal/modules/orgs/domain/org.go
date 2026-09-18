// Package domain holds the orgs module's entities and rules: organisations,
// members and invitations (ADR-0048).
package domain

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// MaxNameLength bounds organisation names. The migration's CHECK matches it.
const MaxNameLength = 100

// PersonalName is the name of a new personal workspace.
const PersonalName = "Personal"

// Org is an organisation. A personal workspace is an organisation too, with
// one owner and no invitations.
type Org struct {
	// ID is an organisation ID such as org_2x7… (orgs.ID in the other
	// layers).
	ID        string
	Name      string
	Personal  bool
	CreatedBy string
	// Version increases with every change.
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
	// DeletedAt is set when an owner deletes the organisation; it is purged
	// after PurgeAfter unless restored.
	DeletedAt  *time.Time
	PurgeAfter *time.Time
}

// Membership is an organisation seen by one of its members.
type Membership struct {
	Org
	Role string
}

// Member is a user's membership, with their email address for display.
type Member struct {
	OrgID    string
	UserID   string
	Email    string
	Role     string
	JoinedAt time.Time
	AddedBy  string
}

// SoleOwnership is an organisation whose only owner is one user.
type SoleOwnership struct {
	OrgID    string
	Personal bool
	Members  int
}

// NormalizeName trims name and checks it, or returns ErrInvalidName.
func NormalizeName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > MaxNameLength || strings.ContainsFunc(name, unicode.IsControl) {
		return "", ErrInvalidName
	}
	return name, nil
}

// NewOrg returns a new organisation created by userID.
func NewOrg(id, name, userID string, personal bool, now time.Time) (Org, error) {
	name, err := NormalizeName(name)
	if err != nil {
		return Org{}, err
	}
	return Org{ID: id, Name: name, Personal: personal, CreatedBy: userID, Version: 1, CreatedAt: now, UpdatedAt: now}, nil
}

// Invitation invites an email address to join an organisation with a role.
type Invitation struct {
	ID              string
	OrgID           string
	Email           string
	NormalizedEmail string
	Role            string
	// TokenHash is the SHA-256 of the token in the invitation link.
	TokenHash  []byte
	InvitedBy  string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	AcceptedAt *time.Time
	RevokedAt  *time.Time
}

// PendingAt reports whether the invitation can still be accepted at now.
func (i Invitation) PendingAt(now time.Time) bool {
	return i.AcceptedAt == nil && i.RevokedAt == nil && now.Before(i.ExpiresAt)
}
