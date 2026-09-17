package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"example.com/mobile-backend/internal/modules/trips/domain"
)

func TestNewTrip(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		name   string
		fields domain.Fields
		want   error
	}{
		{"a trip", domain.Fields{Destination: "Lisbon", Notes: "Three nights."}, nil},
		{"no notes", domain.Fields{Destination: "Lisbon"}, nil},
		{"no destination", domain.Fields{Notes: "Somewhere."}, domain.ErrDestinationRequired},
		{"blank destination", domain.Fields{Destination: "   "}, domain.ErrDestinationRequired},
		{"a long destination", domain.Fields{Destination: strings.Repeat("a", domain.MaxDestination+1)}, domain.ErrDestinationRequired},
		{"long notes", domain.Fields{Destination: "Lisbon", Notes: strings.Repeat("a", domain.MaxNotes+1)}, domain.ErrNotesTooLong},
	} {
		t.Run(tc.name, func(t *testing.T) {
			trip, err := domain.NewTrip("trp_1", "auth0|ada", tc.fields, now)
			if !errors.Is(err, tc.want) {
				t.Fatalf("NewTrip = %v, want %v", err, tc.want)
			}
			if err != nil {
				return
			}
			if !trip.BelongsTo("auth0|ada") || trip.BelongsTo("auth0|bo") {
				t.Errorf("BelongsTo: trip of %q is not only its owner's", trip.OwnerID)
			}
			if trip.Destination != strings.TrimSpace(tc.fields.Destination) {
				t.Errorf("Destination = %q, want it trimmed", trip.Destination)
			}
		})
	}
}

// TestBelongsToRefusesNoOwner: a caller without an ID owns nothing, whatever
// the rows say.
func TestBelongsToRefusesNoOwner(t *testing.T) {
	if (domain.Trip{}).BelongsTo("") {
		t.Error("an empty owner matched a trip without an owner")
	}
}
