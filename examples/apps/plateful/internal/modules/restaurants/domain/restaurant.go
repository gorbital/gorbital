// Package domain holds the restaurants module's restaurants and their
// rules. It imports only the standard library.
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
	MaxAddressLength = 200
	MaxCuisineLength = 60
	MaxReasonLength  = 500
	MaxImageIDLength = 64
	// MinutesInDay is the largest opening or closing minute: 1440 is
	// midnight at the end of the day, so a kitchen open until midnight
	// doesn't have to write 0.
	MinutesInDay = 1440
)

// docs:start restaurant-status

// Status is where a restaurant is in its life on the platform.
type Status string

// Statuses. A restaurant starts onboarding while its staff fill the profile
// in; they publish it (open) and pause it (paused) themselves. Only
// platform staff suspend one, and only they lift a suspension.
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

// SetByStaff reports whether a restaurant's own staff may move to v.
// Suspension is the platform's, so it isn't one of these.
func (v Status) SetByStaff() bool {
	switch v {
	case StatusOnboarding, StatusOpen, StatusPaused:
		return true
	}
	return false
}

// docs:end restaurant-status

// A Restaurant is one organisation's place: the profile customers see and
// the switch that decides whether it takes orders.
type Restaurant struct {
	ID    string
	OrgID string
	// CreatedBy is the member who created the profile, for display and
	// audit. Access comes only from membership.
	CreatedBy string
	RestaurantFields
	Status Status
	// SuspendedReason is why platform staff suspended it, empty otherwise.
	SuspendedReason string
	// Version increases with every change. An update must name the version
	// it read, so it can't overwrite a change it hasn't seen.
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// RestaurantFields are the fields the restaurant's own staff set.
type RestaurantFields struct {
	Name    string
	Address string
	Cuisine string
	// OpensMinute and ClosesMinute are minutes from midnight UTC, 0 to
	// MinutesInDay. Opening after closing means the kitchen works past
	// midnight, which is ordinary for a delivery restaurant.
	OpensMinute  int
	ClosesMinute int
	// DeliveryRadiusM is how far it delivers, in metres, at most the
	// restaurants.max_delivery_radius_m setting.
	DeliveryRadiusM int
	// CoverImageID is the image customers see, or "" while there is none.
	// The images module owns the image and the object behind it; this only
	// names one, and an image that has gone leaves the restaurant without a
	// cover rather than breaking it.
	CoverImageID string
}

// NewRestaurant returns a new restaurant of orgID created by createdBy, or a
// *ValidationError. It starts onboarding: nobody can order from it until its
// staff publish it. maxRadius is the platform's cap on DeliveryRadiusM.
func NewRestaurant(id, orgID, createdBy string, f RestaurantFields, maxRadius int, now time.Time) (Restaurant, error) {
	r := Restaurant{
		ID: id, OrgID: orgID, CreatedBy: createdBy, RestaurantFields: f.clean(),
		Status: StatusOnboarding, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := r.validate(maxRadius); err != nil {
		return Restaurant{}, err
	}
	return r, nil
}

func (f RestaurantFields) clean() RestaurantFields {
	f.Name = strings.TrimSpace(f.Name)
	f.Address = strings.TrimSpace(f.Address)
	f.Cuisine = strings.TrimSpace(f.Cuisine)
	f.CoverImageID = strings.TrimSpace(f.CoverImageID)
	return f
}

// docs:start restaurant-apply

// Apply returns the restaurant with f and status applied, and the names of
// the fields that changed, or a *ValidationError. UpdatedAt becomes now
// only when something changed; the repository increments the version when
// it saves.
//
// Two rules live here rather than in the handler. A suspended restaurant is
// frozen: its staff can't edit their way out of a suspension, they get
// ErrRestaurantSuspended until platform staff lift it. And a status only
// platform staff may set (suspended) is refused with
// ErrSuspensionIsPlatformOnly, whatever the request says, because the
// handler that accepts the body has no way to know who is allowed what.
func (r Restaurant) Apply(f RestaurantFields, status Status, maxRadius int, now time.Time) (Restaurant, []string, error) {
	if r.Status == StatusSuspended {
		return r, nil, ErrRestaurantSuspended
	}
	if !status.Valid() {
		return r, nil, &ValidationError{Errors: []FieldError{{Field: "status", Message: "must be onboarding, open or paused"}}}
	}
	if !status.SetByStaff() {
		return r, nil, ErrSuspensionIsPlatformOnly
	}
	next := r
	next.RestaurantFields = f.clean()
	next.Status = status
	if err := next.validate(maxRadius); err != nil {
		return r, nil, err
	}
	changed := r.changedFields(next)
	if len(changed) > 0 {
		next.UpdatedAt = now
	}
	return next, changed, nil
}

// docs:end restaurant-apply

// Suspend stops a restaurant taking orders, whatever it was doing, and
// records why. Only platform staff reach it (usecase.PermSuspend).
func (r Restaurant) Suspend(reason string, now time.Time) (Restaurant, error) {
	reason = strings.TrimSpace(reason)
	if msg := checkText(reason, 1, MaxReasonLength); msg != "" {
		return r, &ValidationError{Errors: []FieldError{{Field: "reason", Message: msg}}}
	}
	if r.Status == StatusSuspended {
		return r, ErrRestaurantSuspended
	}
	next := r
	next.Status, next.SuspendedReason, next.UpdatedAt = StatusSuspended, reason, now
	return next, nil
}

// Lift ends a suspension. The restaurant comes back paused, never open: its
// staff decide when it takes orders again.
func (r Restaurant) Lift(now time.Time) (Restaurant, error) {
	if r.Status != StatusSuspended {
		return r, ErrRestaurantNotSuspended
	}
	next := r
	next.Status, next.SuspendedReason, next.UpdatedAt = StatusPaused, "", now
	return next, nil
}

// Accepting reports whether the restaurant takes orders right now: it is
// open, and the clock is within its hours.
func (r Restaurant) Accepting(now time.Time) bool {
	return r.Status == StatusOpen && r.WithinHours(now)
}

// docs:start within-hours

// WithinHours reports whether now is inside the restaurant's opening hours.
// A restaurant that closes before it opens works past midnight, so the two
// halves of its day are both inside.
func (r Restaurant) WithinHours(now time.Time) bool {
	minute := now.UTC().Hour()*60 + now.UTC().Minute()
	switch {
	case r.OpensMinute == r.ClosesMinute:
		return true // open all day
	case r.OpensMinute < r.ClosesMinute:
		return minute >= r.OpensMinute && minute < r.ClosesMinute
	default:
		return minute >= r.OpensMinute || minute < r.ClosesMinute
	}
}

// docs:end within-hours

func (r Restaurant) changedFields(next Restaurant) []string {
	var changed []string
	for _, f := range []struct {
		name string
		same bool
	}{
		{"name", r.Name == next.Name},
		{"address", r.Address == next.Address},
		{"cuisine", r.Cuisine == next.Cuisine},
		{"opens_minute", r.OpensMinute == next.OpensMinute},
		{"closes_minute", r.ClosesMinute == next.ClosesMinute},
		{"delivery_radius_m", r.DeliveryRadiusM == next.DeliveryRadiusM},
		{"cover_image_id", r.CoverImageID == next.CoverImageID},
		{"status", r.Status == next.Status},
	} {
		if !f.same {
			changed = append(changed, f.name)
		}
	}
	return changed
}

func (r Restaurant) validate(maxRadius int) error {
	var errs []FieldError
	if msg := checkText(r.Name, 1, MaxNameLength); msg != "" {
		errs = append(errs, FieldError{Field: "name", Message: msg})
	}
	if msg := checkText(r.Address, 1, MaxAddressLength); msg != "" {
		errs = append(errs, FieldError{Field: "address", Message: msg})
	}
	if msg := checkText(r.Cuisine, 0, MaxCuisineLength); msg != "" {
		errs = append(errs, FieldError{Field: "cuisine", Message: msg})
	}
	if msg := checkText(r.CoverImageID, 0, MaxImageIDLength); msg != "" {
		errs = append(errs, FieldError{Field: "cover_image_id", Message: msg})
	}
	for _, m := range []struct {
		field string
		value int
	}{{"opens_minute", r.OpensMinute}, {"closes_minute", r.ClosesMinute}} {
		if m.value < 0 || m.value > MinutesInDay {
			errs = append(errs, FieldError{Field: m.field, Message: fmt.Sprintf("must be between 0 and %d minutes from midnight", MinutesInDay)})
		}
	}
	switch {
	case r.DeliveryRadiusM <= 0:
		errs = append(errs, FieldError{Field: "delivery_radius_m", Message: "must be more than 0 metres"})
	case maxRadius > 0 && r.DeliveryRadiusM > maxRadius:
		errs = append(errs, FieldError{Field: "delivery_radius_m", Message: fmt.Sprintf("must be at most %d metres, the platform's limit", maxRadius)})
	}
	if !r.Status.Valid() {
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
