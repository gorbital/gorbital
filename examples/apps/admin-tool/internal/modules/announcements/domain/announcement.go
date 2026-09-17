// Package domain holds the admin tool's announcements and their rules. It
// imports only the standard library.
package domain

import (
	"strings"
	"time"
	"unicode/utf8"
)

// Longest title and body of an announcement, in characters.
const (
	MaxTitle = 200
	MaxBody  = 5000
)

// Announcement is a message staff publish for customers, shown from
// StartsAt until EndsAt.
type Announcement struct {
	ID    string
	Title string
	Body  string
	// PublishedBy is the ID of the staff member who published it.
	PublishedBy string
	StartsAt    time.Time
	EndsAt      time.Time
	CreatedAt   time.Time
}

// Fields are what staff set when publishing an announcement.
type Fields struct {
	Title  string
	Body   string
	EndsAt time.Time
}

// NewAnnouncement returns an announcement with id, published by publisher
// and shown from now, or ErrTitleRequired, ErrBodyRequired or
// ErrInvalidEndsAt.
func NewAnnouncement(id, publisher string, f Fields, now time.Time) (Announcement, error) {
	a := Announcement{
		ID: id, Title: strings.TrimSpace(f.Title), Body: strings.TrimSpace(f.Body), PublishedBy: publisher,
		StartsAt: now, EndsAt: f.EndsAt.UTC(), CreatedAt: now,
	}
	switch {
	case a.Title == "" || utf8.RuneCountInString(a.Title) > MaxTitle:
		return Announcement{}, ErrTitleRequired
	case a.Body == "" || utf8.RuneCountInString(a.Body) > MaxBody:
		return Announcement{}, ErrBodyRequired
	case !a.EndsAt.After(now):
		return Announcement{}, ErrInvalidEndsAt
	}
	return a, nil
}

// Active reports whether a is shown at now.
func (a Announcement) Active(now time.Time) bool {
	return !now.Before(a.StartsAt) && now.Before(a.EndsAt)
}
