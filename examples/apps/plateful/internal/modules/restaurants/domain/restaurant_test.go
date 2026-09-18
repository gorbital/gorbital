package domain_test

import (
	"errors"
	"testing"
	"time"

	"example.com/plateful/internal/modules/restaurants/domain"
)

// fields are a valid profile the tests start from.
func fields() domain.RestaurantFields {
	return domain.RestaurantFields{
		Name: "Trattoria Bruno", Address: "12 Market Street, Leeds", Cuisine: "Neapolitan",
		OpensMinute: 11 * 60, ClosesMinute: 22 * 60, DeliveryRadiusM: 3000,
	}
}

var now = time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)

func TestNewRestaurantStartsOnboarding(t *testing.T) {
	r, err := domain.NewRestaurant("org_1", fields(), 10_000, now)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != domain.StatusOnboarding || r.Version != 1 {
		t.Errorf("new restaurant = %s version %d, want onboarding version 1", r.Status, r.Version)
	}
	if r.Accepting(now) {
		t.Error("a restaurant being onboarded accepts orders; it must not")
	}
}

func TestNewRestaurantRules(t *testing.T) {
	for name, change := range map[string]func(*domain.RestaurantFields){
		"blank name":     func(f *domain.RestaurantFields) { f.Name = "   " },
		"blank address":  func(f *domain.RestaurantFields) { f.Address = "" },
		"no radius":      func(f *domain.RestaurantFields) { f.DeliveryRadiusM = 0 },
		"radius too big": func(f *domain.RestaurantFields) { f.DeliveryRadiusM = 50_000 },
		"opens too late": func(f *domain.RestaurantFields) { f.OpensMinute = 2000 },
	} {
		t.Run(name, func(t *testing.T) {
			f := fields()
			change(&f)
			if _, err := domain.NewRestaurant("org_1", f, 10_000, now); !errors.Is(err, domain.ErrInvalidRestaurant) {
				t.Errorf("NewRestaurant = %v, want an invalid restaurant", err)
			}
		})
	}
}

// docs:start test-status-rules

// TestStatusRules: the two rules the domain keeps so no handler has to. A
// restaurant's own staff can't set the suspended status, and a suspended
// restaurant is frozen until platform staff lift the suspension.
func TestStatusRules(t *testing.T) {
	r, err := domain.NewRestaurant("org_1", fields(), 10_000, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Apply(fields(), domain.StatusSuspended, 10_000, now); !errors.Is(err, domain.ErrSuspensionIsPlatformOnly) {
		t.Errorf("staff setting suspended = %v, want ErrSuspensionIsPlatformOnly", err)
	}

	open, changed, err := r.Apply(fields(), domain.StatusOpen, 10_000, now)
	if err != nil || len(changed) != 1 || changed[0] != "status" {
		t.Fatalf("publishing = %v, changed %v", err, changed)
	}
	if !open.Accepting(now) {
		t.Error("an open restaurant within its hours doesn't accept orders")
	}

	suspended, err := open.Suspend("selling food it doesn't have", now)
	if err != nil {
		t.Fatal(err)
	}
	if suspended.Accepting(now) {
		t.Error("a suspended restaurant accepts orders")
	}
	if _, _, err := suspended.Apply(fields(), domain.StatusOpen, 10_000, now); !errors.Is(err, domain.ErrRestaurantSuspended) {
		t.Errorf("a suspended restaurant editing itself open = %v, want ErrRestaurantSuspended", err)
	}
	lifted, err := suspended.Lift(now)
	if err != nil || lifted.Status != domain.StatusPaused || lifted.SuspendedReason != "" {
		t.Errorf("lifting = %s %q %v, want paused with no reason", lifted.Status, lifted.SuspendedReason, err)
	}
}

// docs:end test-status-rules

// TestWithinHours covers a kitchen that works past midnight, which is the
// case a single comparison gets wrong.
func TestWithinHours(t *testing.T) {
	at := func(hour, minute int) time.Time {
		return time.Date(2026, 9, 18, hour, minute, 0, 0, time.UTC)
	}
	night := domain.Restaurant{RestaurantFields: domain.RestaurantFields{OpensMinute: 18 * 60, ClosesMinute: 2 * 60}}
	day := domain.Restaurant{RestaurantFields: domain.RestaurantFields{OpensMinute: 11 * 60, ClosesMinute: 22 * 60}}
	always := domain.Restaurant{}
	for name, tt := range map[string]struct {
		r    domain.Restaurant
		when time.Time
		want bool
	}{
		"night, evening":   {night, at(20, 0), true},
		"night, after one": {night, at(1, 30), true},
		"night, lunchtime": {night, at(13, 0), false},
		"day, lunchtime":   {day, at(13, 0), true},
		"day, after close": {day, at(23, 0), false},
		"always open":      {always, at(4, 0), true},
	} {
		t.Run(name, func(t *testing.T) {
			if got := tt.r.WithinHours(tt.when); got != tt.want {
				t.Errorf("WithinHours = %v, want %v", got, tt.want)
			}
		})
	}
}
