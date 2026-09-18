package repository

import (
	"context"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/menus/domain"
)

// selectOpenRestaurantSQL reads the restaurant's columns of orgs, which the
// restaurants module owns. Reading another module's columns in SQL is
// allowed here, and is the right call: the alternative is for this module to
// import the restaurants module's use cases, which the layering forbids, or
// to keep its own copy of which restaurants are open, which would be a
// second truth about the same fact. The columns it reads are the ones the
// app's table contract fixes, and it reads them, never writes them.
//
// The status is part of the WHERE clause rather than something checked
// afterwards, so a restaurant that is onboarding, paused or suspended comes
// back as not found and a suspension can't be told apart from a typo.
const selectOpenRestaurantSQL = `
	SELECT true FROM orgs
	WHERE id = $1 AND status = 'open' AND profile_created_at IS NOT NULL AND deleted_at IS NULL`

// SelectOpenRestaurant returns nil when the restaurant id is open, or
// ErrRestaurantNotFound.
func (s *Store) SelectOpenRestaurant(ctx context.Context, id string) error {
	var open bool
	err := s.db.QueryRow(ctx, selectOpenRestaurantSQL, id).Scan(&open)
	if postgres.IsNoRows(err) {
		return domain.ErrRestaurantNotFound
	}
	return err
}
