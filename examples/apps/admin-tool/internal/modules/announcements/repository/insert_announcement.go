package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"example.com/admin-tool/internal/modules/announcements/domain"
)

const insertAnnouncementSQL = `
	INSERT INTO announcements (id, title, body, published_by, starts_at, ends_at, created_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7)
	RETURNING ` + announcementColumns

// InsertAnnouncement stores a new announcement.
func (s *Store) InsertAnnouncement(ctx context.Context, a domain.Announcement) (domain.Announcement, error) {
	rows, err := s.db.Query(ctx, insertAnnouncementSQL,
		a.ID, a.Title, a.Body, a.PublishedBy, a.StartsAt, a.EndsAt, a.CreatedAt)
	if err != nil {
		return domain.Announcement{}, err
	}
	return pgx.CollectExactlyOneRow(rows, scanAnnouncement)
}
