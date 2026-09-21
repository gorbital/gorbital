// Package domain holds the club books module's club books and their rules. It
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
	MaxTitleLength  = 100
	MaxAuthorLength = 100
	MaxNoteLength   = 2000
)

// Status is one of the values a club book's status can have.
type Status string

// Status values.
const (
	StatusProposed Status = "proposed"
	StatusReading  Status = "reading"
	StatusFinished Status = "finished"
)

// Valid reports whether v is a known status.
func (v Status) Valid() bool {
	switch v {
	case StatusProposed, StatusReading, StatusFinished:
		return true
	}
	return false
}

// ClubBook belongs to one organisation. Its members reach it through their
// role; nobody else can see or change it.
type ClubBook struct {
	ID    string
	OrgID string
	// CreatedBy is the member who created it, a user or the organisation's
	// service account, for display and audit. Access comes only from
	// membership.
	CreatedBy string
	ClubBookFields
	// Version increases with every change. An update must name the version
	// it read, so it can't overwrite a change it hasn't seen.
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ClubBookFields are the fields users set.
type ClubBookFields struct {
	Title  string
	Author string
	Status Status
	Note   string
}

// NewClubBook returns a new club book of orgID created by createdBy, or a
// *ValidationError. Text is trimmed, and an empty choice takes its first
// value.
func NewClubBook(id, orgID, createdBy string, f ClubBookFields, now time.Time) (ClubBook, error) {
	f.Title = strings.TrimSpace(f.Title)
	f.Author = strings.TrimSpace(f.Author)
	f.Note = strings.TrimSpace(f.Note)
	if f.Status == "" {
		f.Status = StatusProposed
	}
	clubBook := ClubBook{ID: id, OrgID: orgID, CreatedBy: createdBy, ClubBookFields: f, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := clubBook.validate(); err != nil {
		return ClubBook{}, err
	}
	return clubBook, nil
}

// Changes are the fields an update sets. Nil fields keep their value.
type Changes struct {
	Title  *string
	Author *string
	Status *Status
	Note   *string
}

// Apply returns clubBook with c applied and the names of the fields whose value
// changed, or a *ValidationError. UpdatedAt becomes now only when a field
// changed. The repository increments the version when it saves.
func (clubBook ClubBook) Apply(c Changes, now time.Time) (ClubBook, []string, error) {
	next := clubBook
	var changed []string
	if c.Title != nil {
		if value := strings.TrimSpace(*c.Title); value != clubBook.Title {
			next.Title, changed = value, append(changed, "title")
		}
	}
	if c.Author != nil {
		if value := strings.TrimSpace(*c.Author); value != clubBook.Author {
			next.Author, changed = value, append(changed, "author")
		}
	}
	if c.Status != nil && *c.Status != clubBook.Status {
		next.Status, changed = *c.Status, append(changed, "status")
	}
	if c.Note != nil {
		if value := strings.TrimSpace(*c.Note); value != clubBook.Note {
			next.Note, changed = value, append(changed, "note")
		}
	}
	if err := next.validate(); err != nil {
		return clubBook, nil, err
	}
	if len(changed) > 0 {
		next.UpdatedAt = now
	}
	return next, changed, nil
}

func (clubBook ClubBook) validate() error {
	var errs []FieldError
	if msg := checkText(clubBook.Title, 1, MaxTitleLength); msg != "" {
		errs = append(errs, FieldError{Field: "title", Message: msg})
	}
	if msg := checkText(clubBook.Author, 0, MaxAuthorLength); msg != "" {
		errs = append(errs, FieldError{Field: "author", Message: msg})
	}
	if !clubBook.Status.Valid() {
		errs = append(errs, FieldError{Field: "status", Message: "must be proposed, reading or finished"})
	}
	if msg := checkText(clubBook.Note, 0, MaxNoteLength); msg != "" {
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
