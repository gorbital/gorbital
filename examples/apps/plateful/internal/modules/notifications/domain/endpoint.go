// Package domain holds the notifications module's endpoints, the messages
// they carry and the rules about which targets this app is willing to talk
// to. It imports only the standard library.
package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Field limits. The migration's CHECK constraints match them.
const (
	MaxLabelLength = 60
	MaxErrorLength = 500
	// MaxTitleLength and MaxLineLength bound what a caller may put in a
	// message, and MaxLines how many lines it may have. The message travels
	// as job arguments, so an unbounded one would be an unbounded row in the
	// job table as well as an unbounded request to someone else's server.
	MaxTitleLength = 200
	MaxLineLength  = 500
	MaxLines       = 20
)

// An Endpoint is one place an organisation wants to be told things: a
// Slack-style incoming webhook, with a label its staff chose and the outcome
// of the last delivery so they can see whether it still works.
type Endpoint struct {
	ID    string
	OrgID string
	// CreatedBy is the member who registered it, for display and audit.
	CreatedBy string
	Label     string
	// URL is the target. It is a [URL] rather than a string so that printing
	// an Endpoint cannot leak it: see [URL.Secret].
	URL URL
	// LastDeliveryAt is when a delivery was last attempted, zero until the
	// first one.
	LastDeliveryAt time.Time
	// LastStatus is the HTTP status the endpoint answered last, 0 when it
	// never answered.
	LastStatus int
	// LastError is why the last delivery failed, empty when it worked. It
	// names the host and the status, never the URL: this field is returned
	// by the API.
	LastError string
	// Version increases with every change, a recorded delivery included.
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewEndpoint returns a new endpoint of orgID registered by createdBy, or a
// *ValidationError for a bad label, or an error wrapping [ErrInvalidURL] for
// a target p refuses.
func NewEndpoint(id, orgID, createdBy, label, rawURL string, p Policy, now time.Time) (Endpoint, error) {
	label = strings.TrimSpace(label)
	if msg := checkText(label, 1, MaxLabelLength); msg != "" {
		return Endpoint{}, &ValidationError{Errors: []FieldError{{Field: "label", Message: msg}}}
	}
	target, err := ParseURL(rawURL, p)
	if err != nil {
		return Endpoint{}, err
	}
	return Endpoint{
		ID: id, OrgID: orgID, CreatedBy: createdBy, Label: label, URL: target,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}, nil
}

// Delivered returns the endpoint with the outcome of one delivery attempt
// recorded: the time, the status the endpoint answered (0 when it never
// answered) and why it failed, empty when it worked. failure is truncated to
// [MaxErrorLength], which is what the column holds.
func (e Endpoint) Delivered(status int, failure string, now time.Time) Endpoint {
	next := e
	next.LastDeliveryAt = now
	next.LastStatus = status
	next.LastError = truncate(strings.TrimSpace(failure), MaxErrorLength)
	next.UpdatedAt = now
	next.Version = e.Version + 1
	return next
}

// docs:start notification-message

// A Message is what one organisation is being told: a title, such as "New
// order", and the lines under it. It is shaped this way because every
// channel worth supporting can render it — a Slack-style webhook takes one
// block of text, an email takes a subject and a body — and because the
// caller should not have to know which one a restaurant registered.
type Message struct {
	Title string
	Lines []string
}

// Text renders the message as the single block of text a Slack-style
// incoming webhook takes: the title, then one line each. Long values are
// truncated rather than refused, because a notification that arrives cut
// short is better than one that doesn't arrive.
func (m Message) Text() string {
	lines := m.Lines
	if len(lines) > MaxLines {
		lines = lines[:MaxLines]
	}
	parts := make([]string, 0, len(lines)+1)
	if title := strings.TrimSpace(m.Title); title != "" {
		parts = append(parts, truncate(title, MaxTitleLength))
	}
	for _, line := range lines {
		if line = strings.TrimSpace(line); line != "" {
			parts = append(parts, truncate(line, MaxLineLength))
		}
	}
	return strings.Join(parts, "\n")
}

// docs:end notification-message

// IsEmpty reports whether the message would render as nothing, which is
// worth catching before it becomes a request to somebody else's server.
func (m Message) IsEmpty() bool { return m.Text() == "" }

// truncate returns the first limit characters of s.
func truncate(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	return string([]rune(s)[:limit])
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
