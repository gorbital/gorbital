package repository

import "context"

const countBooksSQL = `SELECT count(*) FROM books WHERE owner_id = $1`

// CountBooks returns how many books are on ownerID's shelf.
func (s *Store) CountBooks(ctx context.Context, ownerID string) (int, error) {
	var n int
	err := s.db.QueryRow(ctx, countBooksSQL, ownerID).Scan(&n)
	return n, driverError(err)
}
