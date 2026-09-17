// Package repository stores announcements in PostgreSQL with hand-written
// SQL, one file per operation. The table comes from db/migrations.
package repository

import (
	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/admin-tool/internal/modules/announcements/domain"
	"example.com/admin-tool/internal/modules/announcements/usecase"
)

// Store implements usecase.Store on a pool or a transaction.
type Store struct {
	db postgres.DBTX
}

var _ usecase.Store = (*Store)(nil)

// NewStore returns a store on db.
func NewStore(db postgres.DBTX) *Store { return &Store{db: db} }

// announcementColumns are the columns scanAnnouncement reads, in its order.
const announcementColumns = `id, title, body, published_by, starts_at, ends_at, created_at`

func scanAnnouncement(row pgx.CollectableRow) (domain.Announcement, error) {
	var a domain.Announcement
	err := row.Scan(&a.ID, &a.Title, &a.Body, &a.PublishedBy, &a.StartsAt, &a.EndsAt, &a.CreatedAt)
	a.StartsAt, a.EndsAt, a.CreatedAt = a.StartsAt.UTC(), a.EndsAt.UTC(), a.CreatedAt.UTC()
	return a, err
}
