// Package domain holds the mobile backend's trips and their rules. It
// imports only the standard library.
package domain

import (
	"strings"
	"time"
	"unicode/utf8"
)

// Longest destination and notes of a trip, in characters.
const (
	MaxDestination = 120
	MaxNotes       = 2000
)

// A Trip is one traveller's journey, kept for them alone.
type Trip struct {
	ID string
	// OwnerID is the traveller the trip belongs to: the actor's ID, which
	// is the subject of the token the provider issued them.
	OwnerID     string
	Destination string
	Notes       string
	CreatedAt   time.Time
}

// Fields are what a traveller sends when they add a trip.
type Fields struct {
	Destination string
	Notes       string
}

// NewTrip returns a trip with id, belonging to owner, or
// ErrDestinationRequired or ErrNotesTooLong.
func NewTrip(id, owner string, f Fields, now time.Time) (Trip, error) {
	t := Trip{
		ID: id, OwnerID: owner,
		Destination: strings.TrimSpace(f.Destination), Notes: strings.TrimSpace(f.Notes),
		CreatedAt: now,
	}
	switch {
	case t.Destination == "" || utf8.RuneCountInString(t.Destination) > MaxDestination:
		return Trip{}, ErrDestinationRequired
	case utf8.RuneCountInString(t.Notes) > MaxNotes:
		return Trip{}, ErrNotesTooLong
	}
	return t, nil
}

// BelongsTo reports whether the trip is owner's. The queries filter by
// owner already; this is the rule they implement, in one place.
func (t Trip) BelongsTo(owner string) bool { return owner != "" && t.OwnerID == owner }
