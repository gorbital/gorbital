package domain_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"example.com/invoicing/internal/modules/invoices/domain"
)

var now = time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

// validFields returns fields that pass every rule.
func validFields() domain.InvoiceFields {
	return domain.InvoiceFields{Number: "Example number", Customer: "Example customer", Status: domain.StatusDraft, Note: "Example note"}
}

func TestNewInvoice(t *testing.T) {
	f := validFields()
	f.Number = "  " + f.Number + "  "
	f.Customer = "  " + f.Customer + "  "
	f.Note = "  " + f.Note + "  "
	f.Status = ""
	invoice, err := domain.NewInvoice("inv_1", "org_1", "usr_1", f, now)
	if err != nil {
		t.Fatal(err)
	}
	if invoice.InvoiceFields != validFields() || invoice.ID != "inv_1" || invoice.OrgID != "org_1" || invoice.CreatedBy != "usr_1" || invoice.Version != 1 ||
		!invoice.CreatedAt.Equal(now) || !invoice.UpdatedAt.Equal(now) {
		t.Errorf("NewInvoice() = %+v, want trimmed text and default choices", invoice)
	}
}

func TestNewInvoiceValidates(t *testing.T) {
	type fields = domain.InvoiceFields
	tests := []struct {
		name       string
		edit       func(f *fields)
		wantFields []string
	}{
		{"blank number", func(f *fields) { f.Number = "   " }, []string{"number"}},
		{"number at the limit", func(f *fields) { f.Number = strings.Repeat("é", domain.MaxNumberLength) }, nil},
		{"number too long", func(f *fields) { f.Number = strings.Repeat("é", domain.MaxNumberLength+1) }, []string{"number"}},
		{"number with a NUL character", func(f *fields) { f.Number = "ok\x00" }, []string{"number"}},
		{"blank customer", func(f *fields) { f.Customer = "   " }, []string{"customer"}},
		{"customer at the limit", func(f *fields) { f.Customer = strings.Repeat("é", domain.MaxCustomerLength) }, nil},
		{"customer too long", func(f *fields) { f.Customer = strings.Repeat("é", domain.MaxCustomerLength+1) }, []string{"customer"}},
		{"customer with a NUL character", func(f *fields) { f.Customer = "ok\x00" }, []string{"customer"}},
		{"unknown status", func(f *fields) { f.Status = "?" }, []string{"status"}},
		{"note at the limit", func(f *fields) { f.Note = strings.Repeat("é", domain.MaxNoteLength) }, nil},
		{"note too long", func(f *fields) { f.Note = strings.Repeat("é", domain.MaxNoteLength+1) }, []string{"note"}},
		{"note with a NUL character", func(f *fields) { f.Note = "ok\x00" }, []string{"note"}},
		{"every field invalid", func(f *fields) { *f = fields{Number: "\x00", Customer: "\x00", Status: "?", Note: "\x00"} }, []string{"number", "customer", "status", "note"}},
	}
	for _, tt := range tests {
		f := validFields()
		tt.edit(&f)
		_, err := domain.NewInvoice("inv_1", "org_1", "usr_1", f, now)
		if tt.wantFields == nil {
			if err != nil {
				t.Errorf("%s: NewInvoice() error = %v", tt.name, err)
			}
			continue
		}
		var invalid *domain.ValidationError
		if !errors.As(err, &invalid) || !errors.Is(err, domain.ErrInvalidInvoice) {
			t.Errorf("%s: NewInvoice() error = %v, want a ValidationError", tt.name, err)
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
	invoice, err := domain.NewInvoice("inv_1", "org_1", "usr_1", validFields(), now)
	if err != nil {
		t.Fatal(err)
	}
	later := now.Add(time.Hour)
	title, same, blank := " "+invoice.Number+" v2 ", invoice.Number, ""
	choice := domain.StatusVoid

	next, changed, err := invoice.Apply(domain.Changes{Number: &title, Status: &choice}, later)
	if err != nil || next.Number != invoice.Number+" v2" || next.Status != choice || !next.UpdatedAt.Equal(later) ||
		!slices.Equal(changed, []string{"number", "status"}) {
		t.Errorf("Apply() = %+v, %v, %v", next, changed, err)
	}

	unchanged, changed, err := invoice.Apply(domain.Changes{Number: &same}, later)
	if err != nil || len(changed) != 0 || unchanged != invoice {
		t.Errorf("Apply(same number) = %+v, %v, %v; want no change", unchanged, changed, err)
	}

	kept, _, err := invoice.Apply(domain.Changes{Number: &blank}, later)
	if !errors.Is(err, domain.ErrInvalidInvoice) || kept != invoice {
		t.Errorf("Apply(blank number) = %+v, %v; want the original and a validation error", kept, err)
	}
}
