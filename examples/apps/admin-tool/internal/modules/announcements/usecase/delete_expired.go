package usecase

import (
	"context"
	"time"
)

// docs:start delete-expired

// DeleteExpired removes up to limit announcements that ended before before,
// and returns how many it removed. The built-in retention job calls it
// daily, with before the announcements.retention setting ago, until it
// returns fewer than limit.
func (s *Service) DeleteExpired(ctx context.Context, before time.Time, limit int) (int64, error) {
	n, err := s.store.DeleteExpired(ctx, before, limit)
	if err != nil {
		return 0, storeError("delete expired", err)
	}
	return n, nil
}

// docs:end delete-expired
