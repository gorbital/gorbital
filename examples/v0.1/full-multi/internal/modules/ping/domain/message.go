// Package domain holds the ping module's business rules. It imports only the
// standard library.
package domain

import (
	"errors"
	"strings"
)

// ErrMessageRequired is returned when a message is blank.
var ErrMessageRequired = errors.New("message is required")

// A Message is a non-blank, trimmed text.
type Message struct {
	text string
}

// NewMessage validates text and returns a Message.
func NewMessage(text string) (Message, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Message{}, ErrMessageRequired
	}
	return Message{text: text}, nil
}

// Text returns the message text.
func (m Message) Text() string { return m.text }
