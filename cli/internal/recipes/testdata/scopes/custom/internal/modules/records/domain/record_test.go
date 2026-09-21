package domain_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"example.com/app/internal/modules/records/domain"
)

var now = time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

// validFields returns fields that pass every rule.
func validFields() domain.RecordFields {
	return domain.RecordFields{Title: "Example title", Note: "Example note", State: domain.StateOpen}
}

func TestNewRecord(t *testing.T) {
	f := validFields()
	f.Title = "  " + f.Title + "  "
	f.Note = "  " + f.Note + "  "
	f.State = ""
	record, err := domain.NewRecord("rcr_1", "usr_1", f, now)
	if err != nil {
		t.Fatal(err)
	}
	if record.RecordFields != validFields() || record.ID != "rcr_1" || record.CreatedBy != "usr_1" || record.Version != 1 ||
		!record.CreatedAt.Equal(now) || !record.UpdatedAt.Equal(now) {
		t.Errorf("NewRecord() = %+v, want trimmed text and default choices", record)
	}
}

func TestNewRecordValidates(t *testing.T) {
	type fields = domain.RecordFields
	tests := []struct {
		name       string
		edit       func(f *fields)
		wantFields []string
	}{
		{"blank title", func(f *fields) { f.Title = "   " }, []string{"title"}},
		{"title at the limit", func(f *fields) { f.Title = strings.Repeat("é", domain.MaxTitleLength) }, nil},
		{"title too long", func(f *fields) { f.Title = strings.Repeat("é", domain.MaxTitleLength+1) }, []string{"title"}},
		{"title with a NUL character", func(f *fields) { f.Title = "ok\x00" }, []string{"title"}},
		{"note at the limit", func(f *fields) { f.Note = strings.Repeat("é", domain.MaxNoteLength) }, nil},
		{"note too long", func(f *fields) { f.Note = strings.Repeat("é", domain.MaxNoteLength+1) }, []string{"note"}},
		{"note with a NUL character", func(f *fields) { f.Note = "ok\x00" }, []string{"note"}},
		{"unknown state", func(f *fields) { f.State = "?" }, []string{"state"}},
		{"every field invalid", func(f *fields) { *f = fields{Title: "\x00", Note: "\x00", State: "?"} }, []string{"title", "note", "state"}},
	}
	for _, tt := range tests {
		f := validFields()
		tt.edit(&f)
		_, err := domain.NewRecord("rcr_1", "usr_1", f, now)
		if tt.wantFields == nil {
			if err != nil {
				t.Errorf("%s: NewRecord() error = %v", tt.name, err)
			}
			continue
		}
		var invalid *domain.ValidationError
		if !errors.As(err, &invalid) || !errors.Is(err, domain.ErrInvalidRecord) {
			t.Errorf("%s: NewRecord() error = %v, want a ValidationError", tt.name, err)
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
	record, err := domain.NewRecord("rcr_1", "usr_1", validFields(), now)
	if err != nil {
		t.Fatal(err)
	}
	later := now.Add(time.Hour)
	title, same, blank := " "+record.Title+" v2 ", record.Title, ""
	choice := domain.StateDone

	next, changed, err := record.Apply(domain.Changes{Title: &title, State: &choice}, later)
	if err != nil || next.Title != record.Title+" v2" || next.State != choice || !next.UpdatedAt.Equal(later) ||
		!slices.Equal(changed, []string{"title", "state"}) {
		t.Errorf("Apply() = %+v, %v, %v", next, changed, err)
	}

	unchanged, changed, err := record.Apply(domain.Changes{Title: &same}, later)
	if err != nil || len(changed) != 0 || unchanged != record {
		t.Errorf("Apply(same title) = %+v, %v, %v; want no change", unchanged, changed, err)
	}

	kept, _, err := record.Apply(domain.Changes{Title: &blank}, later)
	if !errors.Is(err, domain.ErrInvalidRecord) || kept != record {
		t.Errorf("Apply(blank title) = %+v, %v; want the original and a validation error", kept, err)
	}
}
