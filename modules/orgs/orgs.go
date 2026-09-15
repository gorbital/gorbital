// Package orgs holds the building blocks for organisations in multi-tenant
// apps (ADR-0023, ADR-0048): organisation IDs, the membership check every
// organisation operation starts with, and invitation emails. The generated
// app owns the flows, tables and SQL in internal/modules/orgs.
//
// Stability: pre-1.0 (ADR-0015).
package orgs

import (
	"crypto/rand"
	"encoding/base32"
	"errors"
	"strings"
)

// The organisation roles every app declares in its org catalog. Apps may
// add their own. Role names are public API.
const (
	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleMember = "member"
)

// Errors. Check them with [errors.Is].
var (
	// ErrOrgNotFound reports an organisation that doesn't exist, is deleted,
	// or that the caller isn't a member of. The three look the same, so
	// organisation IDs can't be probed.
	ErrOrgNotFound = errors.New("orgs: organisation not found")
	// ErrNotMember is returned by [Memberships] when the user isn't a
	// member of a live organisation. [RequireMember] turns it into
	// [ErrOrgNotFound].
	ErrNotMember = errors.New("orgs: not a member")
	// ErrInvalidID reports a string that isn't an organisation ID.
	ErrInvalidID = errors.New("orgs: invalid organisation ID")
)

// idPrefix starts every organisation ID.
const idPrefix = "org_"

var idEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// idLength is the prefix plus 128 random bits in unpadded base32.
var idLength = len(idPrefix) + idEncoding.EncodedLen(16)

// ID identifies an organisation, such as org_2x7…. It is a distinct type so
// a user ID can't be passed where an organisation ID is expected.
type ID string

// NewID returns a random organisation ID with 128 bits of randomness.
func NewID() ID {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // never fails (crypto/rand)
	return ID(idPrefix + idEncoding.EncodeToString(b))
}

// ParseID returns s as an ID, or [ErrInvalidID] when s isn't shaped like
// one. It doesn't check that the organisation exists.
func ParseID(s string) (ID, error) {
	if len(s) != idLength || !strings.HasPrefix(s, idPrefix) {
		return "", ErrInvalidID
	}
	if _, err := idEncoding.DecodeString(s[len(idPrefix):]); err != nil {
		return "", ErrInvalidID
	}
	return ID(s), nil
}

// String returns the ID as stored.
func (id ID) String() string { return string(id) }
