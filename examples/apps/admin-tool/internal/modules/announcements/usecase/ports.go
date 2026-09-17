package usecase

import (
	"context"
	"time"

	"example.com/admin-tool/internal/modules/announcements/domain"
)

// docs:start store

// Store reads and writes announcements; repository.Store implements it with
// SQL.
type Store interface {
	// InsertAnnouncement stores a new announcement.
	InsertAnnouncement(ctx context.Context, a domain.Announcement) (domain.Announcement, error)
	// SelectActive returns up to limit announcements active at now, newest
	// first.
	SelectActive(ctx context.Context, now time.Time, limit int) ([]domain.Announcement, error)
	// CountActive returns how many announcements are active at now.
	CountActive(ctx context.Context, now time.Time) (int, error)
	// DeleteExpired removes up to limit announcements that ended before
	// before, and returns how many it removed.
	DeleteExpired(ctx context.Context, before time.Time, limit int) (int64, error)
	// OldestExpired returns the earliest end of the announcements that ended
	// before now, and false when none has.
	OldestExpired(ctx context.Context, now time.Time) (time.Time, bool, error)
}

// docs:end store
