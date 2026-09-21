// Package domain holds Shelfie's books and their rules. It imports only
// the standard library.
package domain

import (
	"strings"
	"time"
	"unicode/utf8"
)

// Status is where a book is in its reader's reading.
type Status string

// Statuses of a book.
const (
	WantToRead Status = "want_to_read"
	Reading    Status = "reading"
	Read       Status = "read"
)

// Valid reports whether s is a known status.
func (s Status) Valid() bool { return s == WantToRead || s == Reading || s == Read }

// Book is a book on one reader's shelf.
type Book struct {
	ID      string
	OwnerID string
	Title   string
	Author  string
	// ISBN is an ISBN-10 or ISBN-13 without hyphens, or empty.
	ISBN      string
	Status    Status
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Fields are what a reader sets when adding a book.
type Fields struct {
	Title  string
	Author string
	ISBN   string
	// Status is WantToRead when empty.
	Status Status
}

// Changes are the fields an update sets; nil leaves a field as it is.
type Changes struct {
	Title  *string
	Author *string
	ISBN   *string
	Status *Status
}

// docs:start new-book

// NewBook returns a book with id for ownerID, or ErrTitleRequired,
// ErrInvalidISBN or ErrInvalidStatus.
func NewBook(id, ownerID string, f Fields, now time.Time) (Book, error) {
	b := Book{ID: id, OwnerID: ownerID, Title: f.Title, Author: f.Author, ISBN: f.ISBN, Status: f.Status, CreatedAt: now, UpdatedAt: now}
	if b.Status == "" {
		b.Status = WantToRead
	}
	return b.normalize()
}

// docs:end new-book

// Apply returns b with c applied, validated as NewBook validates.
func (b Book) Apply(c Changes, now time.Time) (Book, error) {
	if c.Title != nil {
		b.Title = *c.Title
	}
	if c.Author != nil {
		b.Author = *c.Author
	}
	if c.ISBN != nil {
		b.ISBN = *c.ISBN
	}
	if c.Status != nil {
		b.Status = *c.Status
	}
	b.UpdatedAt = now
	return b.normalize()
}

// normalize trims the text fields, normalizes the ISBN and checks every
// rule.
func (b Book) normalize() (Book, error) {
	b.Title = strings.TrimSpace(b.Title)
	b.Author = strings.TrimSpace(b.Author)
	switch {
	case b.Title == "" || utf8.RuneCountInString(b.Title) > 300:
		return Book{}, ErrTitleRequired
	case utf8.RuneCountInString(b.Author) > 200:
		return Book{}, ErrAuthorTooLong
	case !b.Status.Valid():
		return Book{}, ErrInvalidStatus
	}
	isbn, err := NormalizeISBN(b.ISBN)
	if err != nil {
		return Book{}, err
	}
	b.ISBN = isbn
	return b, nil
}

// NormalizeISBN removes hyphens and spaces from an ISBN and checks it: ten
// characters, digits with an optional final X, or thirteen digits. An empty
// ISBN stays empty. It returns ErrInvalidISBN for anything else.
func NormalizeISBN(s string) (string, error) {
	s = strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(s))
	if s == "" {
		return "", nil
	}
	switch len(s) {
	case 10, 13:
	default:
		return "", ErrInvalidISBN
	}
	for i, r := range s {
		if r >= '0' && r <= '9' || (r == 'X' && len(s) == 10 && i == 9) {
			continue
		}
		return "", ErrInvalidISBN
	}
	return s, nil
}
