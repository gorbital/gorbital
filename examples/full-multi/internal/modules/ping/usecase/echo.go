package usecase

import (
	"context"

	"example.com/acme-api/internal/modules/ping/domain"
)

// Echo validates text and returns it as a message.
func (s *Service) Echo(_ context.Context, text string) (domain.Message, error) {
	return domain.NewMessage(text)
}
