package domain

import "errors"

// docs:start errors

// Errors of the trips use cases. module.go maps each to an HTTP status and
// a problem code, which are public API.
var (
	// ErrDestinationRequired reports a blank destination, or one over
	// MaxDestination characters.
	ErrDestinationRequired = errors.New("trips: a destination is required, up to 120 characters")
	// ErrNotesTooLong reports notes over MaxNotes characters.
	ErrNotesTooLong = errors.New("trips: the notes are longer than 2000 characters")
	// ErrTripNotFound reports that the caller has no trip with this ID.
	// Another traveller's trip is not found rather than forbidden, so an ID
	// tells a caller nothing about anyone else's trips.
	ErrTripNotFound = errors.New("trips: no trip with this ID")
)

// docs:end errors
