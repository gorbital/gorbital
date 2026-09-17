// Package route holds the route configuration that the gorbital and guard
// packages both set, so guards can be route options without the gorbital
// package exporting its internals.
package route

// Config is what a route's options set before it is registered.
type Config struct {
	Tags        []string
	Summary     string
	Description string
	OperationID string
	// Status is the success status; zero keeps Huma's default.
	Status int
	// Errors are error statuses the route documents.
	Errors     []int
	Deprecated bool
	// Public removes the authenticated-actor check and the security
	// requirement (ADR-0082).
	Public bool
}

// Option sets part of a route's configuration.
type Option func(*Config)
