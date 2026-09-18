package domain_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"example.com/plateful/internal/modules/projects/domain"
)

var now = time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

// validFields returns fields that pass every rule.
func validFields() domain.ProjectFields {
	return domain.ProjectFields{Name: "Example name", Description: "Example description", Status: domain.StatusActive}
}

func TestNewProject(t *testing.T) {
	f := validFields()
	f.Name = "  " + f.Name + "  "
	f.Description = "  " + f.Description + "  "
	f.Status = ""
	project, err := domain.NewProject("prj_1", "org_1", "usr_1", f, now)
	if err != nil {
		t.Fatal(err)
	}
	if project.ProjectFields != validFields() || project.ID != "prj_1" || project.OrgID != "org_1" || project.CreatedBy != "usr_1" || project.Version != 1 ||
		!project.CreatedAt.Equal(now) || !project.UpdatedAt.Equal(now) {
		t.Errorf("NewProject() = %+v, want trimmed text and default choices", project)
	}
}

func TestNewProjectValidates(t *testing.T) {
	type fields = domain.ProjectFields
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
		{"unknown status", func(f *fields) { f.Status = "?" }, []string{"status"}},
		{"every field invalid", func(f *fields) { *f = fields{Name: "\x00", Description: "\x00", Status: "?"} }, []string{"name", "description", "status"}},
	}
	for _, tt := range tests {
		f := validFields()
		tt.edit(&f)
		_, err := domain.NewProject("prj_1", "org_1", "usr_1", f, now)
		if tt.wantFields == nil {
			if err != nil {
				t.Errorf("%s: NewProject() error = %v", tt.name, err)
			}
			continue
		}
		var invalid *domain.ValidationError
		if !errors.As(err, &invalid) || !errors.Is(err, domain.ErrInvalidProject) {
			t.Errorf("%s: NewProject() error = %v, want a ValidationError", tt.name, err)
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
	project, err := domain.NewProject("prj_1", "org_1", "usr_1", validFields(), now)
	if err != nil {
		t.Fatal(err)
	}
	later := now.Add(time.Hour)
	title, same, blank := " "+project.Name+" v2 ", project.Name, ""
	choice := domain.StatusArchived

	next, changed, err := project.Apply(domain.Changes{Name: &title, Status: &choice}, later)
	if err != nil || next.Name != project.Name+" v2" || next.Status != choice || !next.UpdatedAt.Equal(later) ||
		!slices.Equal(changed, []string{"name", "status"}) {
		t.Errorf("Apply() = %+v, %v, %v", next, changed, err)
	}

	unchanged, changed, err := project.Apply(domain.Changes{Name: &same}, later)
	if err != nil || len(changed) != 0 || unchanged != project {
		t.Errorf("Apply(same name) = %+v, %v, %v; want no change", unchanged, changed, err)
	}

	kept, _, err := project.Apply(domain.Changes{Name: &blank}, later)
	if !errors.Is(err, domain.ErrInvalidProject) || kept != project {
		t.Errorf("Apply(blank name) = %+v, %v; want the original and a validation error", kept, err)
	}
}
