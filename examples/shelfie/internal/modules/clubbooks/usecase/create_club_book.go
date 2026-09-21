package usecase

import (
	"context"

	"example.com/shelfie/internal/modules/clubbooks/domain"
)

// CreateClubBook adds a club book to the organisation orgID, created by the member
// acting in it.
func (s *Service) CreateClubBook(ctx context.Context, orgID string, f domain.ClubBookFields) (domain.ClubBook, error) {
	member, err := memberID(ctx, orgID)
	if err != nil {
		return domain.ClubBook{}, err
	}
	clubBook, err := domain.NewClubBook(s.newID(), orgID, member, f, s.clock())
	if err != nil {
		return domain.ClubBook{}, err
	}
	created, err := s.store.InsertClubBook(ctx, clubBook)
	if err != nil {
		return domain.ClubBook{}, storeError("create", err)
	}
	s.audit(ctx, ActionCreated, created.ID, nil)
	return created, nil
}
