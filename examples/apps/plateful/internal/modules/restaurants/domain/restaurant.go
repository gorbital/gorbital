// Package domain holds the restaurants module's restaurants and their rules. It
// imports only the standard library.
package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Field limits. The migration's CHECK constraints match them.
const (
	MaxNameLength    = 100
	MaxAddressLength = 100
	MaxCuisineLength = 100
)

// Status is one of the values a restaurant's status can have.
type Status string

// Status values.
const (
	StatusOnboarding Status = "onboarding"
	StatusOpen       Status = "open"
	StatusPaused     Status = "paused"
	StatusSuspended  Status = "suspended"
)

// Valid reports whether v is a known status.
func (v Status) Valid() bool {
	switch v {
	case StatusOnboarding, StatusOpen, StatusPaused, StatusSuspended:
		return true
	}
	return false
}

// Restaurant belongs to one organisation. Its members reach it through their
// role; nobody else can see or change it.
type Restaurant struct {
	ID    string
	OrgID string
	// CreatedBy is the member who created it, a user or the organisation's
	// service account, for display and audit. Access comes only from
	// membership.
	CreatedBy string
	RestaurantFields
	// Version increases with every change. An update must name the version
	// it read, so it can't overwrite a change it hasn't seen.
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// RestaurantFields are the fields users set.
type RestaurantFields struct {
	Name    string
	Address string
	Cuisine string
	Status  Status
}

// NewRestaurant returns a new restaurant of orgID created by createdBy, or a
// *ValidationError. Text is trimmed, and an empty choice takes its first
// value.
func NewRestaurant(id, orgID, createdBy string, f RestaurantFields, now time.Time) (Restaurant, error) {
	f.Name = strings.TrimSpace(f.Name)
	f.Address = strings.TrimSpace(f.Address)
	f.Cuisine = strings.TrimSpace(f.Cuisine)
	if f.Status == "" {
		f.Status = StatusOnboarding
	}
	restaurant := Restaurant{ID: id, OrgID: orgID, CreatedBy: createdBy, RestaurantFields: f, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := restaurant.validate(); err != nil {
		return Restaurant{}, err
	}
	return restaurant, nil
}

// Changes are the fields an update sets. Nil fields keep their value.
type Changes struct {
	Name    *string
	Address *string
	Cuisine *string
	Status  *Status
}

// Apply returns restaurant with c applied and the names of the fields whose value
// changed, or a *ValidationError. UpdatedAt becomes now only when a field
// changed. The repository increments the version when it saves.
func (restaurant Restaurant) Apply(c Changes, now time.Time) (Restaurant, []string, error) {
	next := restaurant
	var changed []string
	if c.Name != nil {
		if value := strings.TrimSpace(*c.Name); value != restaurant.Name {
			next.Name, changed = value, append(changed, "name")
		}
	}
	if c.Address != nil {
		if value := strings.TrimSpace(*c.Address); value != restaurant.Address {
			next.Address, changed = value, append(changed, "address")
		}
	}
	if c.Cuisine != nil {
		if value := strings.TrimSpace(*c.Cuisine); value != restaurant.Cuisine {
			next.Cuisine, changed = value, append(changed, "cuisine")
		}
	}
	if c.Status != nil && *c.Status != restaurant.Status {
		next.Status, changed = *c.Status, append(changed, "status")
	}
	if err := next.validate(); err != nil {
		return restaurant, nil, err
	}
	if len(changed) > 0 {
		next.UpdatedAt = now
	}
	return next, changed, nil
}

func (restaurant Restaurant) validate() error {
	var errs []FieldError
	if msg := checkText(restaurant.Name, 1, MaxNameLength); msg != "" {
		errs = append(errs, FieldError{Field: "name", Message: msg})
	}
	if msg := checkText(restaurant.Address, 1, MaxAddressLength); msg != "" {
		errs = append(errs, FieldError{Field: "address", Message: msg})
	}
	if msg := checkText(restaurant.Cuisine, 1, MaxCuisineLength); msg != "" {
		errs = append(errs, FieldError{Field: "cuisine", Message: msg})
	}
	if !restaurant.Status.Valid() {
		errs = append(errs, FieldError{Field: "status", Message: "must be onboarding, open, paused or suspended"})
	}
	if len(errs) > 0 {
		return &ValidationError{Errors: errs}
	}
	return nil
}

// checkText returns why s isn't a valid text of min to max characters, or "".
func checkText(s string, minLen, maxLen int) string {
	switch n := utf8.RuneCountInString(s); {
	case !utf8.ValidString(s) || strings.ContainsRune(s, 0):
		return "must be valid text"
	case n < minLen:
		return "is required"
	case n > maxLen:
		return fmt.Sprintf("must be at most %d characters", maxLen)
	}
	return ""
}
