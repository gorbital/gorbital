package usecase

import (
	"context"

	"example.com/shelfie/internal/modules/profiles/domain"
)

// GetProfile returns the signed-in reader's profile, or ErrProfileIncomplete
// when they haven't created one, as after a first Google sign-in.
func (s *Service) GetProfile(ctx context.Context) (domain.Profile, error) {
	reader, err := readerID(ctx)
	if err != nil {
		return domain.Profile{}, err
	}
	p, err := s.store.SelectProfile(ctx, reader)
	if err != nil {
		return domain.Profile{}, storeError("get", err)
	}
	return p, nil
}
