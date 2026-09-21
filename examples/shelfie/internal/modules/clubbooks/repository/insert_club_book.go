package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"example.com/shelfie/internal/modules/clubbooks/domain"
)

const insertClubBookSQL = `
	INSERT INTO club_books (id, org_id, created_by, title, author, status, note, version, created_at, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	RETURNING ` + clubBookColumns

// InsertClubBook stores a new club book, or returns ErrClubBookTitleTaken when the
// organisation already uses the value, ignoring case.
func (s *Store) InsertClubBook(ctx context.Context, clubBook domain.ClubBook) (domain.ClubBook, error) {
	rows, err := s.db.Query(ctx, insertClubBookSQL,
		clubBook.ID, clubBook.OrgID, clubBook.CreatedBy, clubBook.Title, clubBook.Author, clubBook.Status, clubBook.Note, clubBook.Version, clubBook.CreatedAt, clubBook.UpdatedAt)
	if err == nil {
		clubBook, err = pgx.CollectExactlyOneRow(rows, scanClubBook)
	}
	return clubBook, constraintError(err)
}
