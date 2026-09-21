package repository

import "context"

const deleteBooksSQL = `DELETE FROM books WHERE owner_id = $1`

// DeleteBooks removes every one of ownerID's books and returns how many it
// removed; an empty shelf is not an error.
func (s *Store) DeleteBooks(ctx context.Context, ownerID string) (int, error) {
	tag, err := s.db.Exec(ctx, deleteBooksSQL, ownerID)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}
