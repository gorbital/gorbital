package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"example.com/plateful/internal/modules/menus/domain"
)

// selectMenuSQL reads a whole published menu in one statement: the
// organisation's available dishes in the order a menu is printed. The order
// is the menu_items_org_section_position index exactly, so the database
// walks the index and the use case groups the rows into sections in one
// pass, without sorting and without a second query per section.
//
// There is no LIMIT: a menu is a document a restaurant means to be read
// whole, and one that was long enough to need paging would be unreadable
// for the customer long before it was slow for us.
const selectMenuSQL = `
	SELECT ` + itemColumns + ` FROM menu_items
	WHERE org_id = $1 AND available
	ORDER BY section, position, id`

// SelectMenu returns the organisation's available items in menu order.
func (s *Store) SelectMenu(ctx context.Context, orgID string) ([]domain.Item, error) {
	rows, err := s.db.Query(ctx, selectMenuSQL, orgID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanItem)
}
