package usecase

import (
	"context"
	"errors"

	"example.com/shelfie/internal/modules/profiles/domain"
)

// docs:start check-sign-in

// Suspended reports whether a reader's profile is suspended. A reader
// without a profile isn't.
func (s *Service) Suspended(ctx context.Context, userID string) (bool, error) {
	p, err := s.store.SelectProfile(ctx, userID)
	switch {
	case errors.Is(err, domain.ErrProfileIncomplete):
		return false, nil
	case err != nil:
		return false, err
	}
	return p.Suspended, nil
}

// docs:end check-sign-in
