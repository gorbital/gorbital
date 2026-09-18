// Package domain holds the couriers module's couriers and their rules. It
// imports only the standard library.
//
// A courier belongs to no organisation, so nothing here carries an
// organisation: the only owner a courier has is the sign-in account in
// UserID, and the use case is what compares it with the caller.
package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxDisplayNameLength is the longest display name. The migration's CHECK
// constraint matches it.
const MaxDisplayNameLength = 80

// docs:start courier-vehicle

// Vehicle is what a courier carries orders on. It decides how far a
// restaurant will send them, so it is the one thing besides the name a
// dispatcher sees.
type Vehicle string

// Vehicles. The migration's CHECK constraint lists the same four.
const (
	VehicleBicycle Vehicle = "bicycle"
	VehicleScooter Vehicle = "scooter"
	VehicleCar     Vehicle = "car"
	VehicleOnFoot  Vehicle = "on_foot"
)

// Valid reports whether v is a known vehicle.
func (v Vehicle) Valid() bool {
	switch v {
	case VehicleBicycle, VehicleScooter, VehicleCar, VehicleOnFoot:
		return true
	}
	return false
}

// docs:end courier-vehicle

// A Courier is one person who delivers on the platform.
type Courier struct {
	ID string
	// UserID is the sign-in account the courier signs in with, and the only
	// thing that says whose profile this is: there is no organisation to ask
	// instead. One profile per account.
	UserID      string
	DisplayName string
	Vehicle     Vehicle
	// Available is whether the courier is working right now, which is theirs
	// to switch and nobody else's.
	Available bool
	// ActiveOrderID is the order they are carrying, empty when they are
	// free. The orders module owns this field and writes it in its own
	// transaction; this module only reads it, to keep a courier from going
	// off duty half way through a delivery.
	ActiveOrderID string
	// Version increases with every change. An update must name the version
	// it read, so it can't overwrite a change it hasn't seen.
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Changes are the fields a courier may change about themselves. A nil field
// is left as it is.
type Changes struct {
	DisplayName *string
	Vehicle     *Vehicle
	Available   *bool
}

// NewCourier returns a courier profile for the account userID, or a
// *ValidationError. A new courier starts unavailable and carrying nothing:
// registering says who you are, going on duty is a separate decision.
func NewCourier(id, userID, displayName string, vehicle Vehicle, now time.Time) (Courier, error) {
	c := Courier{
		ID: id, UserID: userID, DisplayName: strings.TrimSpace(displayName), Vehicle: vehicle,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := c.validate(); err != nil {
		return Courier{}, err
	}
	return c, nil
}

// Carrying reports whether the courier has an order on them right now.
func (c Courier) Carrying() bool { return c.ActiveOrderID != "" }

// docs:start courier-owned-by

// OwnedBy reports whether the courier profile is the account userID's own.
//
// This is the whole of a courier's access control, and it is a string
// comparison rather than a guard, because there is no organisation to ask.
// An org-scoped resource is protected before the handler runs:
// guard.OrgMember reads {orgId} out of the path and asks the organisations
// module whether the caller is a member. A courier has no {orgId} and no
// membership, so the equivalent question — is this row yours? — can only be
// answered after the row is read, which means in the use case.
func (c Courier) OwnedBy(userID string) bool { return userID != "" && c.UserID == userID }

// docs:end courier-owned-by

// docs:start courier-apply

// Apply returns the courier with ch applied and the names of the fields
// whose value changed, or a *ValidationError. UpdatedAt becomes now only
// when something changed; the repository increments the version when it
// saves.
//
// One rule lives here rather than in the handler: a courier who is carrying
// an order may not mark themselves unavailable (ErrCourierOnDelivery). Going
// off duty with a diner's food in the bag is the one change a courier can
// make that leaves an order stranded, and the rule belongs next to the
// field, where a job or a command reaches it too. Coming back on duty, and
// changing a name or a vehicle mid-delivery, are all fine.
func (c Courier) Apply(ch Changes, now time.Time) (Courier, []string, error) {
	next := c
	var changed []string
	if ch.DisplayName != nil {
		if value := strings.TrimSpace(*ch.DisplayName); value != c.DisplayName {
			next.DisplayName, changed = value, append(changed, "display_name")
		}
	}
	if ch.Vehicle != nil && *ch.Vehicle != c.Vehicle {
		next.Vehicle, changed = *ch.Vehicle, append(changed, "vehicle")
	}
	if ch.Available != nil && *ch.Available != c.Available {
		if !*ch.Available && c.Carrying() {
			return c, nil, ErrCourierOnDelivery
		}
		next.Available, changed = *ch.Available, append(changed, "available")
	}
	if err := next.validate(); err != nil {
		return c, nil, err
	}
	if len(changed) > 0 {
		next.UpdatedAt = now
	}
	return next, changed, nil
}

// docs:end courier-apply

func (c Courier) validate() error {
	var errs []FieldError
	if msg := checkText(c.DisplayName, 1, MaxDisplayNameLength); msg != "" {
		errs = append(errs, FieldError{Field: "display_name", Message: msg})
	}
	if !c.Vehicle.Valid() {
		errs = append(errs, FieldError{Field: "vehicle", Message: "must be bicycle, scooter, car or on_foot"})
	}
	if len(errs) > 0 {
		return &ValidationError{Errors: errs}
	}
	return nil
}

// checkText returns why s isn't a valid text of minLen to maxLen
// characters, or "".
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
