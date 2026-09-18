package domain_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"example.com/plateful/internal/modules/orders/domain"
)

var now = time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

// validFields returns fields that pass every rule.
func validFields() domain.OrderFields {
	return domain.OrderFields{Status: domain.StatusPlaced, Address: "Example address", Note: "Example note"}
}

func TestNewOrder(t *testing.T) {
	f := validFields()
	f.Address = "  " + f.Address + "  "
	f.Note = "  " + f.Note + "  "
	f.Status = ""
	order, err := domain.NewOrder("ord_1", "org_1", "usr_1", f, now)
	if err != nil {
		t.Fatal(err)
	}
	if order.OrderFields != validFields() || order.ID != "ord_1" || order.OrgID != "org_1" || order.CreatedBy != "usr_1" || order.Version != 1 ||
		!order.CreatedAt.Equal(now) || !order.UpdatedAt.Equal(now) {
		t.Errorf("NewOrder() = %+v, want trimmed text and default choices", order)
	}
}

func TestNewOrderValidates(t *testing.T) {
	type fields = domain.OrderFields
	tests := []struct {
		name       string
		edit       func(f *fields)
		wantFields []string
	}{
		{"unknown status", func(f *fields) { f.Status = "?" }, []string{"status"}},
		{"blank address", func(f *fields) { f.Address = "   " }, []string{"address"}},
		{"address at the limit", func(f *fields) { f.Address = strings.Repeat("é", domain.MaxAddressLength) }, nil},
		{"address too long", func(f *fields) { f.Address = strings.Repeat("é", domain.MaxAddressLength+1) }, []string{"address"}},
		{"address with a NUL character", func(f *fields) { f.Address = "ok\x00" }, []string{"address"}},
		{"note at the limit", func(f *fields) { f.Note = strings.Repeat("é", domain.MaxNoteLength) }, nil},
		{"note too long", func(f *fields) { f.Note = strings.Repeat("é", domain.MaxNoteLength+1) }, []string{"note"}},
		{"note with a NUL character", func(f *fields) { f.Note = "ok\x00" }, []string{"note"}},
		{"every field invalid", func(f *fields) { *f = fields{Status: "?", Address: "\x00", Note: "\x00"} }, []string{"status", "address", "note"}},
	}
	for _, tt := range tests {
		f := validFields()
		tt.edit(&f)
		_, err := domain.NewOrder("ord_1", "org_1", "usr_1", f, now)
		if tt.wantFields == nil {
			if err != nil {
				t.Errorf("%s: NewOrder() error = %v", tt.name, err)
			}
			continue
		}
		var invalid *domain.ValidationError
		if !errors.As(err, &invalid) || !errors.Is(err, domain.ErrInvalidOrder) {
			t.Errorf("%s: NewOrder() error = %v, want a ValidationError", tt.name, err)
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
	order, err := domain.NewOrder("ord_1", "org_1", "usr_1", validFields(), now)
	if err != nil {
		t.Fatal(err)
	}
	later := now.Add(time.Hour)
	title, same, blank := " "+order.Address+" v2 ", order.Address, ""
	choice := domain.StatusCancelled

	next, changed, err := order.Apply(domain.Changes{Address: &title, Status: &choice}, later)
	if err != nil || next.Address != order.Address+" v2" || next.Status != choice || !next.UpdatedAt.Equal(later) ||
		!slices.Equal(changed, []string{"status", "address"}) {
		t.Errorf("Apply() = %+v, %v, %v", next, changed, err)
	}

	unchanged, changed, err := order.Apply(domain.Changes{Address: &same}, later)
	if err != nil || len(changed) != 0 || unchanged != order {
		t.Errorf("Apply(same address) = %+v, %v, %v; want no change", unchanged, changed, err)
	}

	kept, _, err := order.Apply(domain.Changes{Address: &blank}, later)
	if !errors.Is(err, domain.ErrInvalidOrder) || kept != order {
		t.Errorf("Apply(blank address) = %+v, %v; want the original and a validation error", kept, err)
	}
}
