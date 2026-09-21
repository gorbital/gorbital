package repository

import (
	"context"

	"example.com/shelfie/internal/modules/clubbooks/domain"
)

const deleteClubBookSQL = `DELETE FROM club_books WHERE id = $1 AND org_id = $2`

// DeleteClubBook removes one of orgID's club books, or returns
// ErrClubBookNotFound.
func (s *Store) DeleteClubBook(ctx context.Context, orgID, id string) error {
	tag, err := s.db.Exec(ctx, deleteClubBookSQL, id, orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrClubBookNotFound
	}
	return nil
}
