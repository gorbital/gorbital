package usecase

import (
	"context"

	"example.com/shelfie/internal/modules/profiles/domain"
)

// docs:start save-registration

// SaveRegistration creates the profile of a reader who just registered,
// from the fields they sent with POST /v1/auth/register. It runs in the
// transaction that creates the account. The fields were validated when the
// request arrived; Clean can only fail here if the rules drifted apart, and
// then the account is rolled back.
func (s *Service) SaveRegistration(ctx context.Context, userID string, f domain.Fields) error {
	f, err := f.Clean()
	if err != nil {
		return err
	}
	now := s.clock()
	_, err = s.store.UpsertProfile(ctx, domain.Profile{UserID: userID, DisplayName: f.DisplayName, Country: f.Country, CreatedAt: now, UpdatedAt: now})
	return err
}

// docs:end save-registration
