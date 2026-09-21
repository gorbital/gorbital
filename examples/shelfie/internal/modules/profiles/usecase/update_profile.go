package usecase

import (
	"context"

	"example.com/shelfie/internal/modules/profiles/domain"
)

// docs:start update-profile

// UpdateProfile creates or changes the signed-in reader's profile: how a
// reader who signed up with Google, Apple or GitHub completes theirs.
func (s *Service) UpdateProfile(ctx context.Context, f domain.Fields) (domain.Profile, error) {
	reader, err := readerID(ctx)
	if err != nil {
		return domain.Profile{}, err
	}
	if f, err = f.Clean(); err != nil {
		return domain.Profile{}, err
	}
	now := s.clock()
	p, err := s.store.UpsertProfile(ctx, domain.Profile{UserID: reader, DisplayName: f.DisplayName, Country: f.Country, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		return domain.Profile{}, storeError("update", err)
	}
	return p, nil
}

// docs:end update-profile
