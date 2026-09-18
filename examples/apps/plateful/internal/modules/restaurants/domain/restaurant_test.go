package domain_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"example.com/plateful/internal/modules/restaurants/domain"
)

var now = time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

// validFields returns fields that pass every rule.
func validFields() domain.RestaurantFields {
	return domain.RestaurantFields{Name: "Example name", Address: "Example address", Cuisine: "Example cuisine", Status: domain.StatusOnboarding}
}

func TestNewRestaurant(t *testing.T) {
	f := validFields()
	f.Name = "  " + f.Name + "  "
	f.Address = "  " + f.Address + "  "
	f.Cuisine = "  " + f.Cuisine + "  "
	f.Status = ""
	restaurant, err := domain.NewRestaurant("rst_1", "org_1", "usr_1", f, now)
	if err != nil {
		t.Fatal(err)
	}
	if restaurant.RestaurantFields != validFields() || restaurant.ID != "rst_1" || restaurant.OrgID != "org_1" || restaurant.CreatedBy != "usr_1" || restaurant.Version != 1 ||
		!restaurant.CreatedAt.Equal(now) || !restaurant.UpdatedAt.Equal(now) {
		t.Errorf("NewRestaurant() = %+v, want trimmed text and default choices", restaurant)
	}
}

func TestNewRestaurantValidates(t *testing.T) {
	type fields = domain.RestaurantFields
	tests := []struct {
		name       string
		edit       func(f *fields)
		wantFields []string
	}{
		{"blank name", func(f *fields) { f.Name = "   " }, []string{"name"}},
		{"name at the limit", func(f *fields) { f.Name = strings.Repeat("é", domain.MaxNameLength) }, nil},
		{"name too long", func(f *fields) { f.Name = strings.Repeat("é", domain.MaxNameLength+1) }, []string{"name"}},
		{"name with a NUL character", func(f *fields) { f.Name = "ok\x00" }, []string{"name"}},
		{"blank address", func(f *fields) { f.Address = "   " }, []string{"address"}},
		{"address at the limit", func(f *fields) { f.Address = strings.Repeat("é", domain.MaxAddressLength) }, nil},
		{"address too long", func(f *fields) { f.Address = strings.Repeat("é", domain.MaxAddressLength+1) }, []string{"address"}},
		{"address with a NUL character", func(f *fields) { f.Address = "ok\x00" }, []string{"address"}},
		{"blank cuisine", func(f *fields) { f.Cuisine = "   " }, []string{"cuisine"}},
		{"cuisine at the limit", func(f *fields) { f.Cuisine = strings.Repeat("é", domain.MaxCuisineLength) }, nil},
		{"cuisine too long", func(f *fields) { f.Cuisine = strings.Repeat("é", domain.MaxCuisineLength+1) }, []string{"cuisine"}},
		{"cuisine with a NUL character", func(f *fields) { f.Cuisine = "ok\x00" }, []string{"cuisine"}},
		{"unknown status", func(f *fields) { f.Status = "?" }, []string{"status"}},
		{"every field invalid", func(f *fields) { *f = fields{Name: "\x00", Address: "\x00", Cuisine: "\x00", Status: "?"} }, []string{"name", "address", "cuisine", "status"}},
	}
	for _, tt := range tests {
		f := validFields()
		tt.edit(&f)
		_, err := domain.NewRestaurant("rst_1", "org_1", "usr_1", f, now)
		if tt.wantFields == nil {
			if err != nil {
				t.Errorf("%s: NewRestaurant() error = %v", tt.name, err)
			}
			continue
		}
		var invalid *domain.ValidationError
		if !errors.As(err, &invalid) || !errors.Is(err, domain.ErrInvalidRestaurant) {
			t.Errorf("%s: NewRestaurant() error = %v, want a ValidationError", tt.name, err)
			continue
		}
		var got []string
		for _, fe := range invalid.Errors {
			got = append(got, fe.Field)
		}
		if !slices.Equal(got, tt.wantFields) {
			t.Errorf("%s: invalid fields = %v, want %v", tt.name, got, tt.wantFields)
		}
	}
}

func TestApply(t *testing.T) {
	restaurant, err := domain.NewRestaurant("rst_1", "org_1", "usr_1", validFields(), now)
	if err != nil {
		t.Fatal(err)
	}
	later := now.Add(time.Hour)
	title, same, blank := " "+restaurant.Name+" v2 ", restaurant.Name, ""
	choice := domain.StatusSuspended

	next, changed, err := restaurant.Apply(domain.Changes{Name: &title, Status: &choice}, later)
	if err != nil || next.Name != restaurant.Name+" v2" || next.Status != choice || !next.UpdatedAt.Equal(later) ||
		!slices.Equal(changed, []string{"name", "status"}) {
		t.Errorf("Apply() = %+v, %v, %v", next, changed, err)
	}

	unchanged, changed, err := restaurant.Apply(domain.Changes{Name: &same}, later)
	if err != nil || len(changed) != 0 || unchanged != restaurant {
		t.Errorf("Apply(same name) = %+v, %v, %v; want no change", unchanged, changed, err)
	}

	kept, _, err := restaurant.Apply(domain.Changes{Name: &blank}, later)
	if !errors.Is(err, domain.ErrInvalidRestaurant) || kept != restaurant {
		t.Errorf("Apply(blank name) = %+v, %v; want the original and a validation error", kept, err)
	}
}
