// Package usecase holds the ping module's application logic: the service in
// this file, and one file per operation.
package usecase

import (
	"gorbital.dev/config"
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
