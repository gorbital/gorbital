// Package domain holds the records module's records and their rules. It
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
	MaxTitleLength = 100
	MaxNoteLength  = 2000
)

// State is one of the values a record's state can have.
type State string

// State values.
const (
	StateOpen State = "open"
	StateDone State = "done"
)

// Valid reports whether v is a known state.
func (v State) Valid() bool {
	switch v {
	case StateOpen, StateDone:
		return true
	}
	return false
}

// Record belongs to one organisation. Its members reach it through their
// role; nobody else can see or change it.
type Record struct {
	ID    string
	OrgID string
	// CreatedBy is the member who created it, a user or the organisation's
	// service account, for display and audit. Access comes only from
	// membership.
	CreatedBy string
	RecordFields
	// Version increases with every change. An update must name the version
	// it read, so it can't overwrite a change it hasn't seen.
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// RecordFields are the fields users set.
type RecordFields struct {
	Title string
	Note  string
	State State
}

// NewRecord returns a new record of orgID created by createdBy, or a
// *ValidationError. Text is trimmed, and an empty choice takes its first
// value.
func NewRecord(id, orgID, createdBy string, f RecordFields, now time.Time) (Record, error) {
	f.Title = strings.TrimSpace(f.Title)
	f.Note = strings.TrimSpace(f.Note)
	if f.State == "" {
		f.State = StateOpen
	}
	record := Record{ID: id, OrgID: orgID, CreatedBy: createdBy, RecordFields: f, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := record.validate(); err != nil {
		return Record{}, err
	}
	return record, nil
}

// Changes are the fields an update sets. Nil fields keep their value.
type Changes struct {
	Title *string
	Note  *string
	State *State
}

// Apply returns record with c applied and the names of the fields whose value
// changed, or a *ValidationError. UpdatedAt becomes now only when a field
// changed. The repository increments the version when it saves.
func (record Record) Apply(c Changes, now time.Time) (Record, []string, error) {
	next := record
	var changed []string
	if c.Title != nil {
		if value := strings.TrimSpace(*c.Title); value != record.Title {
			next.Title, changed = value, append(changed, "title")
		}
	}
	if c.Note != nil {
		if value := strings.TrimSpace(*c.Note); value != record.Note {
			next.Note, changed = value, append(changed, "note")
		}
	}
	if c.State != nil && *c.State != record.State {
		next.State, changed = *c.State, append(changed, "state")
	}
	if err := next.validate(); err != nil {
		return record, nil, err
	}
	if len(changed) > 0 {
		next.UpdatedAt = now
	}
	return next, changed, nil
}

func (record Record) validate() error {
	var errs []FieldError
	if msg := checkText(record.Title, 1, MaxTitleLength); msg != "" {
		errs = append(errs, FieldError{Field: "title", Message: msg})
	}
	if msg := checkText(record.Note, 0, MaxNoteLength); msg != "" {
		errs = append(errs, FieldError{Field: "note", Message: msg})
	}
	if !record.State.Valid() {
		errs = append(errs, FieldError{Field: "state", Message: "must be open or done"})
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
