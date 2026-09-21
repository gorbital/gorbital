package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/shelfie/internal/modules/clubbooks/domain"
)

const updateClubBookSQL = `
	UPDATE club_books
	SET title = $3, author = $4, status = $5, note = $6, updated_at = $7, version = version + 1
	WHERE id = $1 AND org_id = $2 AND version = $8
	RETURNING ` + clubBookColumns

// UpdateClubBook saves clubBook when the stored version is still clubBook.Version and
// returns it with the next version. It returns ErrClubBookVersionConflict when
// no row has that version (changed, deleted or not the organisation's), and
// ErrClubBookTitleTaken.
func (s *Store) UpdateClubBook(ctx context.Context, clubBook domain.ClubBook) (domain.ClubBook, error) {
	rows, err := s.db.Query(ctx, updateClubBookSQL,
		clubBook.ID, clubBook.OrgID, clubBook.Title, clubBook.Author, clubBook.Status, clubBook.Note, clubBook.UpdatedAt, clubBook.Version)
	if err != nil {
		return domain.ClubBook{}, constraintError(err)
	}
	updated, err := pgx.CollectExactlyOneRow(rows, scanClubBook)
	if postgres.IsNoRows(err) {
		return domain.ClubBook{}, domain.ErrClubBookVersionConflict
	}
	return updated, constraintError(err)
}
