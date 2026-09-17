// Package usecase holds the ping module's application logic.
package usecase

import (
	"context"
	"time"

	"gorbital.dev/config"

	pingdomain "example.com/acme-api/internal/modules/ping/domain"
)

// Service runs the ping use cases.
type Service struct {
	message    config.Value[string]
	serverTime config.Value[bool]
}

// NewService returns a Service replying with message, and with the server's
// time while serverTime is on. Both are read on every call, so runtime
// setting and feature flag changes apply immediately.
func NewService(message config.Value[string], serverTime config.Value[bool]) *Service {
	return &Service{message: message, serverTime: serverTime}
}

// Ping reports that the API is reachable.
func (s *Service) Ping(ctx context.Context) string { return s.message.Get(ctx) }

// ServerTime returns the server's time and true when the caller gets it in
// ping replies: an example of a feature rolled out with a flag.
func (s *Service) ServerTime(ctx context.Context) (time.Time, bool) {
	if !s.serverTime.Get(ctx) {
		return time.Time{}, false
	}
	return time.Now().UTC(), true
}

// Echo validates text and returns it as a message.
func (s *Service) Echo(_ context.Context, text string) (pingdomain.Message, error) {
	return pingdomain.NewMessage(text)
}
