package domain_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"example.com/shelfie/internal/modules/clubbooks/domain"
)

var now = time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

// validFields returns fields that pass every rule.
func validFields() domain.ClubBookFields {
	return domain.ClubBookFields{Title: "Example title", Author: "Example author", Status: domain.StatusProposed, Note: "Example note"}
}

func TestNewClubBook(t *testing.T) {
	f := validFields()
	f.Title = "  " + f.Title + "  "
	f.Author = "  " + f.Author + "  "
	f.Note = "  " + f.Note + "  "
	f.Status = ""
	clubBook, err := domain.NewClubBook("clb_1", "org_1", "usr_1", f, now)
	if err != nil {
		t.Fatal(err)
	}
	if clubBook.ClubBookFields != validFields() || clubBook.ID != "clb_1" || clubBook.OrgID != "org_1" || clubBook.CreatedBy != "usr_1" || clubBook.Version != 1 ||
		!clubBook.CreatedAt.Equal(now) || !clubBook.UpdatedAt.Equal(now) {
		t.Errorf("NewClubBook() = %+v, want trimmed text and default choices", clubBook)
	}
}

func TestNewClubBookValidates(t *testing.T) {
	type fields = domain.ClubBookFields
	tests := []struct {
		name       string
		edit       func(f *fields)
		wantFields []string
	}{
		{"blank title", func(f *fields) { f.Title = "   " }, []string{"title"}},
		{"title at the limit", func(f *fields) { f.Title = strings.Repeat("é", domain.MaxTitleLength) }, nil},
		{"title too long", func(f *fields) { f.Title = strings.Repeat("é", domain.MaxTitleLength+1) }, []string{"title"}},
		{"title with a NUL character", func(f *fields) { f.Title = "ok\x00" }, []string{"title"}},
		{"author at the limit", func(f *fields) { f.Author = strings.Repeat("é", domain.MaxAuthorLength) }, nil},
		{"author too long", func(f *fields) { f.Author = strings.Repeat("é", domain.MaxAuthorLength+1) }, []string{"author"}},
		{"author with a NUL character", func(f *fields) { f.Author = "ok\x00" }, []string{"author"}},
		{"unknown status", func(f *fields) { f.Status = "?" }, []string{"status"}},
		{"note at the limit", func(f *fields) { f.Note = strings.Repeat("é", domain.MaxNoteLength) }, nil},
		{"note too long", func(f *fields) { f.Note = strings.Repeat("é", domain.MaxNoteLength+1) }, []string{"note"}},
		{"note with a NUL character", func(f *fields) { f.Note = "ok\x00" }, []string{"note"}},
		{"every field invalid", func(f *fields) { *f = fields{Title: "\x00", Author: "\x00", Status: "?", Note: "\x00"} }, []string{"title", "author", "status", "note"}},
	}
	for _, tt := range tests {
		f := validFields()
		tt.edit(&f)
		_, err := domain.NewClubBook("clb_1", "org_1", "usr_1", f, now)
		if tt.wantFields == nil {
			if err != nil {
				t.Errorf("%s: NewClubBook() error = %v", tt.name, err)
			}
			continue
		}
		var invalid *domain.ValidationError
		if !errors.As(err, &invalid) || !errors.Is(err, domain.ErrInvalidClubBook) {
			t.Errorf("%s: NewClubBook() error = %v, want a ValidationError", tt.name, err)
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
	clubBook, err := domain.NewClubBook("clb_1", "org_1", "usr_1", validFields(), now)
	if err != nil {
		t.Fatal(err)
	}
	later := now.Add(time.Hour)
	title, same, blank := " "+clubBook.Title+" v2 ", clubBook.Title, ""
	choice := domain.StatusFinished

	next, changed, err := clubBook.Apply(domain.Changes{Title: &title, Status: &choice}, later)
	if err != nil || next.Title != clubBook.Title+" v2" || next.Status != choice || !next.UpdatedAt.Equal(later) ||
		!slices.Equal(changed, []string{"title", "status"}) {
		t.Errorf("Apply() = %+v, %v, %v", next, changed, err)
	}

	unchanged, changed, err := clubBook.Apply(domain.Changes{Title: &same}, later)
	if err != nil || len(changed) != 0 || unchanged != clubBook {
		t.Errorf("Apply(same title) = %+v, %v, %v; want no change", unchanged, changed, err)
	}

	kept, _, err := clubBook.Apply(domain.Changes{Title: &blank}, later)
	if !errors.Is(err, domain.ErrInvalidClubBook) || kept != clubBook {
		t.Errorf("Apply(blank title) = %+v, %v; want the original and a validation error", kept, err)
	}
}
