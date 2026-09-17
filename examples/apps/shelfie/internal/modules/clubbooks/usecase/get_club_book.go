package usecase

import (
	"context"

	"example.com/shelfie/internal/modules/clubbooks/domain"
)

// GetClubBook returns one of the organisation orgID's club books.
func (s *Service) GetClubBook(ctx context.Context, orgID, id string) (domain.ClubBook, error) {
	if _, err := memberID(ctx, orgID); err != nil {
		return domain.ClubBook{}, err
	}
	clubBook, err := s.store.SelectClubBook(ctx, orgID, id, false)
	if err != nil {
		return domain.ClubBook{}, storeError("get", err)
	}
	return clubBook, nil
}
