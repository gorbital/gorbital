// Package domain holds the purchases partners report and their rules. It
// imports only the standard library.
package domain

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxTitleLength is the longest title a partner may send. The migration's
// CHECK constraint matches it.
const MaxTitleLength = 300

// Errors of the partner use cases; module.go maps them to problem codes.
var (
	ErrUnauthenticated = errors.New("partners: a signed-in reader is required")
	// ErrUnknownPartner reports a delivery signed with a secret that no
	// partner of this app uses.
	ErrUnknownPartner = errors.New("partners: unknown partner")
	ErrInvalidEventID = errors.New("partners: an event ID is 1 to 100 characters")
	ErrInvalidReader  = errors.New("partners: a purchase names the reader who bought the book")
	ErrInvalidISBN    = errors.New("partners: the ISBN must be 13 digits")
	ErrInvalidTitle   = errors.New("partners: a title is 1 to 300 characters")
	ErrInvalidTime    = errors.New("partners: purchased_at is required and can't be in the future")
)

var (
	isbn13    = regexp.MustCompile(`^[0-9]{13}$`)
	eventIDRE = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,100}$`)
)

// Purchase is a book a partner reported a reader bought.
type Purchase struct {
	ID string
	// Partner names the shop that sent the delivery, from the secret that
	// verified it.
	Partner string
	// EventID is the partner's own ID for the delivery. It is unique per
	// partner, which is what makes recording a purchase idempotent.
	EventID     string
	UserID      string
	ISBN        string
	Title       string
	PurchasedAt time.Time
	CreatedAt   time.Time
}

// Event is what a partner sends.
type Event struct {
	EventID     string
	UserID      string
	ISBN        string
	Title       string
	PurchasedAt time.Time
}

// docs:start new-purchase

// NewPurchase returns the purchase e records for partner, or the first rule
// it breaks. A verified signature proves who sent the delivery; it says
// nothing about what is in it, so every field is checked here.
func NewPurchase(id, partner string, e Event, now time.Time) (Purchase, error) {
	e.Title = strings.TrimSpace(e.Title)
	e.ISBN = strings.ReplaceAll(strings.TrimSpace(e.ISBN), "-", "")
	switch {
	case !eventIDRE.MatchString(e.EventID):
		return Purchase{}, ErrInvalidEventID
	case strings.TrimSpace(e.UserID) == "":
		return Purchase{}, ErrInvalidReader
	case !isbn13.MatchString(e.ISBN):
		return Purchase{}, ErrInvalidISBN
	case e.Title == "" || utf8.RuneCountInString(e.Title) > MaxTitleLength:
		return Purchase{}, ErrInvalidTitle
	case e.PurchasedAt.IsZero() || e.PurchasedAt.After(now):
		return Purchase{}, ErrInvalidTime
	}
	return Purchase{
		ID: id, Partner: partner, EventID: e.EventID, UserID: e.UserID,
		ISBN: e.ISBN, Title: e.Title, PurchasedAt: e.PurchasedAt.UTC(), CreatedAt: now,
	}, nil
}

// docs:end new-purchase
