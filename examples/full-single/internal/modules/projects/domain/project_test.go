package domain_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	projectsdomain "example.com/acme-api/internal/modules/projects/domain"
)

var now = time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

func TestNewProject(t *testing.T) {
	p, err := projectsdomain.NewProject("prj_1", "usr_1", "  Website  ", " New pages ", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Website" || p.Description != "New pages" || p.Status != projectsdomain.StatusActive || p.Version != 1 ||
		!p.CreatedAt.Equal(now) || !p.UpdatedAt.Equal(now) {
		t.Errorf("NewProject() = %+v", p)
	}
}

func TestNewProjectValidates(t *testing.T) {
	tests := []struct {
		name, description string
		status            projectsdomain.Status
		wantFields        []string
	}{
		{name: "   ", wantFields: []string{"name"}},
		{name: strings.Repeat("é", projectsdomain.MaxNameLength)},
		{name: strings.Repeat("é", projectsdomain.MaxNameLength+1), wantFields: []string{"name"}},
		{name: "ok\x00", wantFields: []string{"name"}},
		{name: "ok", description: strings.Repeat("a", projectsdomain.MaxDescriptionLength+1), wantFields: []string{"description"}},
		{name: "", description: string([]byte{0xff}), status: "done", wantFields: []string{"name", "description", "status"}},
	}
	for _, tt := range tests {
		_, err := projectsdomain.NewProject("prj_1", "usr_1", tt.name, tt.description, tt.status, now)
		if tt.wantFields == nil {
			if err != nil {
				t.Errorf("NewProject(%q) error = %v", tt.name, err)
			}
			continue
		}
		var invalid *projectsdomain.ValidationError
		if !errors.As(err, &invalid) || !errors.Is(err, projectsdomain.ErrInvalidProject) {
			t.Errorf("NewProject(%q, %q, %q) error = %v, want a ValidationError", tt.name, tt.description, tt.status, err)
			continue
		}
		var fields []string
		for _, f := range invalid.Errors {
			fields = append(fields, f.Field)
		}
		if !slices.Equal(fields, tt.wantFields) {
			t.Errorf("NewProject(%q, %q, %q) invalid fields = %v, want %v", tt.name, tt.description, tt.status, fields, tt.wantFields)
		}
	}
}

func TestApply(t *testing.T) {
	p, err := projectsdomain.NewProject("prj_1", "usr_1", "Website", "", "", now)
	if err != nil {
		t.Fatal(err)
	}
	later := now.Add(time.Hour)
	name, same, archived := " Website v2 ", "Website", projectsdomain.StatusArchived

	next, changed, err := p.Apply(projectsdomain.Changes{Name: &name, Status: &archived}, later)
	if err != nil || next.Name != "Website v2" || next.Status != archived || !next.UpdatedAt.Equal(later) ||
		!slices.Equal(changed, []string{"name", "status"}) {
		t.Errorf("Apply() = %+v, %v, %v", next, changed, err)
	}

	unchanged, changed, err := p.Apply(projectsdomain.Changes{Name: &same}, later)
	if err != nil || len(changed) != 0 || !unchanged.UpdatedAt.Equal(now) {
		t.Errorf("Apply(same name) = %+v, %v, %v; want no change", unchanged, changed, err)
	}

	blank, bad := "", projectsdomain.Status("done")
	kept, _, err := p.Apply(projectsdomain.Changes{Name: &blank, Status: &bad}, later)
	if !errors.Is(err, projectsdomain.ErrInvalidProject) || kept != p {
		t.Errorf("Apply(invalid) = %+v, %v; want the original and a validation error", kept, err)
	}
}
