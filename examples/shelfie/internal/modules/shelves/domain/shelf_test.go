package domain_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"example.com/shelfie/internal/modules/shelves/domain"
)

var now = time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

// validFields returns fields that pass every rule.
func validFields() domain.ShelfFields {
	return domain.ShelfFields{Name: "Example name", Description: "Example description", Visibility: domain.VisibilityPrivate}
}

func TestNewShelf(t *testing.T) {
	f := validFields()
	f.Name = "  " + f.Name + "  "
	f.Description = "  " + f.Description + "  "
	f.Visibility = ""
	shelf, err := domain.NewShelf("shl_1", "usr_1", f, now)
	if err != nil {
		t.Fatal(err)
	}
	if shelf.ShelfFields != validFields() || shelf.ID != "shl_1" || shelf.OwnerID != "usr_1" || shelf.Version != 1 ||
		!shelf.CreatedAt.Equal(now) || !shelf.UpdatedAt.Equal(now) {
		t.Errorf("NewShelf() = %+v, want trimmed text and default choices", shelf)
	}
}

func TestNewShelfValidates(t *testing.T) {
	type fields = domain.ShelfFields
	tests := []struct {
		name       string
		edit       func(f *fields)
		wantFields []string
	}{
		{"blank name", func(f *fields) { f.Name = "   " }, []string{"name"}},
		{"name at the limit", func(f *fields) { f.Name = strings.Repeat("é", domain.MaxNameLength) }, nil},
		{"name too long", func(f *fields) { f.Name = strings.Repeat("é", domain.MaxNameLength+1) }, []string{"name"}},
		{"name with a NUL character", func(f *fields) { f.Name = "ok\x00" }, []string{"name"}},
		{"description at the limit", func(f *fields) { f.Description = strings.Repeat("é", domain.MaxDescriptionLength) }, nil},
		{"description too long", func(f *fields) { f.Description = strings.Repeat("é", domain.MaxDescriptionLength+1) }, []string{"description"}},
		{"description with a NUL character", func(f *fields) { f.Description = "ok\x00" }, []string{"description"}},
		{"unknown visibility", func(f *fields) { f.Visibility = "?" }, []string{"visibility"}},
		{"every field invalid", func(f *fields) { *f = fields{Name: "\x00", Description: "\x00", Visibility: "?"} }, []string{"name", "description", "visibility"}},
	}
	for _, tt := range tests {
		f := validFields()
		tt.edit(&f)
		_, err := domain.NewShelf("shl_1", "usr_1", f, now)
		if tt.wantFields == nil {
			if err != nil {
				t.Errorf("%s: NewShelf() error = %v", tt.name, err)
			}
			continue
		}
		var invalid *domain.ValidationError
		if !errors.As(err, &invalid) || !errors.Is(err, domain.ErrInvalidShelf) {
			t.Errorf("%s: NewShelf() error = %v, want a ValidationError", tt.name, err)
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
	shelf, err := domain.NewShelf("shl_1", "usr_1", validFields(), now)
	if err != nil {
		t.Fatal(err)
	}
	later := now.Add(time.Hour)
	title, same, blank := " "+shelf.Name+" v2 ", shelf.Name, ""
	choice := domain.VisibilityShared

	next, changed, err := shelf.Apply(domain.Changes{Name: &title, Visibility: &choice}, later)
	if err != nil || next.Name != shelf.Name+" v2" || next.Visibility != choice || !next.UpdatedAt.Equal(later) ||
		!slices.Equal(changed, []string{"name", "visibility"}) {
		t.Errorf("Apply() = %+v, %v, %v", next, changed, err)
	}

	unchanged, changed, err := shelf.Apply(domain.Changes{Name: &same}, later)
	if err != nil || len(changed) != 0 || unchanged != shelf {
		t.Errorf("Apply(same name) = %+v, %v, %v; want no change", unchanged, changed, err)
	}

	kept, _, err := shelf.Apply(domain.Changes{Name: &blank}, later)
	if !errors.Is(err, domain.ErrInvalidShelf) || kept != shelf {
		t.Errorf("Apply(blank name) = %+v, %v; want the original and a validation error", kept, err)
	}
}
