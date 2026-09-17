package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"example.com/admin-tool/internal/modules/announcements/domain"
)

const selectActiveSQL = `
	SELECT ` + announcementColumns + ` FROM announcements
	WHERE starts_at <= $1 AND ends_at > $1
	ORDER BY starts_at DESC, id DESC
	LIMIT $2`

// SelectActive returns up to limit announcements active at now, newest
// first.
func (s *Store) SelectActive(ctx context.Context, now time.Time, limit int) ([]domain.Announcement, error) {
	rows, err := s.db.Query(ctx, selectActiveSQL, now, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanAnnouncement)
}
