package repository

import (
	"context"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/menus/domain"
)

// selectOpenRestaurantOrgSQL reads the restaurants module's own table.
// Reading another module's table in SQL is allowed here, and is the right
// call: the alternative is for this module to import the restaurants
// module's use cases, which the layering forbids, or to keep its own copy of
// which restaurants are open, which would be a second truth about the same
// fact. The columns it reads are the ones the app's table contract fixes,
// and it reads them, never writes them.
//
// The status is part of the WHERE clause rather than something checked
// afterwards, so a restaurant that is onboarding, paused or suspended comes
// back as not found and a suspension can't be told apart from a typo.
const selectOpenRestaurantOrgSQL = `SELECT org_id FROM restaurants WHERE id = $1 AND status = 'open'`

// SelectOpenRestaurantOrg returns the organisation of the restaurant
// restaurantID when it is open, or ErrRestaurantNotFound.
func (s *Store) SelectOpenRestaurantOrg(ctx context.Context, restaurantID string) (string, error) {
	var orgID string
	err := s.db.QueryRow(ctx, selectOpenRestaurantOrgSQL, restaurantID).Scan(&orgID)
	if postgres.IsNoRows(err) {
		return "", domain.ErrRestaurantNotFound
	}
	return orgID, err
}
