package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/shelfie/internal/modules/clubbooks/domain"
)

const (
	selectClubBookSQL = `SELECT ` + clubBookColumns + ` FROM club_books WHERE id = $1 AND org_id = $2`
	forUpdate         = ` FOR UPDATE`
)

// SelectClubBook returns one of orgID's club books, or ErrClubBookNotFound.
// lock locks the row until the transaction ends.
func (s *Store) SelectClubBook(ctx context.Context, orgID, id string, lock bool) (domain.ClubBook, error) {
	sql := selectClubBookSQL
	if lock {
		sql += forUpdate
	}
	rows, err := s.db.Query(ctx, sql, id, orgID)
	if err != nil {
		return domain.ClubBook{}, err
	}
	clubBook, err := pgx.CollectExactlyOneRow(rows, scanClubBook)
	if postgres.IsNoRows(err) {
		return domain.ClubBook{}, domain.ErrClubBookNotFound
	}
	return clubBook, err
}
