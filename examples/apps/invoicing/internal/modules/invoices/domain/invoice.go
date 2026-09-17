// Package domain holds the invoices module's invoices and their rules. It
// imports only the standard library.
package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Field limits. The migration's CHECK constraints match them.
const (
	MaxNumberLength   = 100
	MaxCustomerLength = 100
	MaxNoteLength     = 2000
)

// Status is one of the values an invoice's status can have.
type Status string

// Status values.
const (
	StatusDraft Status = "draft"
	StatusSent  Status = "sent"
	StatusPaid  Status = "paid"
	StatusVoid  Status = "void"
)

// Valid reports whether v is a known status.
func (v Status) Valid() bool {
	switch v {
	case StatusDraft, StatusSent, StatusPaid, StatusVoid:
		return true
	}
	return false
}

// Invoice belongs to one organisation. Its members reach it through their
// role; nobody else can see or change it.
type Invoice struct {
	ID    string
	OrgID string
	// CreatedBy is the member who created it, a user or the organisation's
	// service account, for display and audit. Access comes only from
	// membership.
	CreatedBy string
	InvoiceFields
	// Version increases with every change. An update must name the version
	// it read, so it can't overwrite a change it hasn't seen.
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// InvoiceFields are the fields users set.
type InvoiceFields struct {
	Number   string
	Customer string
	Status   Status
	Note     string
}

// NewInvoice returns a new invoice of orgID created by createdBy, or a
// *ValidationError. Text is trimmed, and an empty choice takes its first
// value.
func NewInvoice(id, orgID, createdBy string, f InvoiceFields, now time.Time) (Invoice, error) {
	f.Number = strings.TrimSpace(f.Number)
	f.Customer = strings.TrimSpace(f.Customer)
	f.Note = strings.TrimSpace(f.Note)
	if f.Status == "" {
		f.Status = StatusDraft
	}
	invoice := Invoice{ID: id, OrgID: orgID, CreatedBy: createdBy, InvoiceFields: f, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := invoice.validate(); err != nil {
		return Invoice{}, err
	}
	return invoice, nil
}

// Changes are the fields an update sets. Nil fields keep their value.
type Changes struct {
	Number   *string
	Customer *string
	Status   *Status
	Note     *string
}

// Apply returns invoice with c applied and the names of the fields whose value
// changed, or a *ValidationError. UpdatedAt becomes now only when a field
// changed. The repository increments the version when it saves.
func (invoice Invoice) Apply(c Changes, now time.Time) (Invoice, []string, error) {
	next := invoice
	var changed []string
	if c.Number != nil {
		if value := strings.TrimSpace(*c.Number); value != invoice.Number {
			next.Number, changed = value, append(changed, "number")
		}
	}
	if c.Customer != nil {
		if value := strings.TrimSpace(*c.Customer); value != invoice.Customer {
			next.Customer, changed = value, append(changed, "customer")
		}
	}
	if c.Status != nil && *c.Status != invoice.Status {
		next.Status, changed = *c.Status, append(changed, "status")
	}
	if c.Note != nil {
		if value := strings.TrimSpace(*c.Note); value != invoice.Note {
			next.Note, changed = value, append(changed, "note")
		}
	}
	if err := next.validate(); err != nil {
		return invoice, nil, err
	}
	if len(changed) > 0 {
		next.UpdatedAt = now
	}
	return next, changed, nil
}

func (invoice Invoice) validate() error {
	var errs []FieldError
	if msg := checkText(invoice.Number, 1, MaxNumberLength); msg != "" {
		errs = append(errs, FieldError{Field: "number", Message: msg})
	}
	if msg := checkText(invoice.Customer, 1, MaxCustomerLength); msg != "" {
		errs = append(errs, FieldError{Field: "customer", Message: msg})
	}
	if !invoice.Status.Valid() {
		errs = append(errs, FieldError{Field: "status", Message: "must be draft, sent, paid or void"})
	}
	if msg := checkText(invoice.Note, 0, MaxNoteLength); msg != "" {
		errs = append(errs, FieldError{Field: "note", Message: msg})
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
