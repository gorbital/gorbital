package usecase

import (
	"context"
	"time"
)

// OldestExpired returns when the announcement that ended first ended, among
// those that have, and false when none has. GET /ops/retention shows it as
// oldest_at.
func (s *Service) OldestExpired(ctx context.Context) (time.Time, bool, error) {
	at, ok, err := s.store.OldestExpired(ctx, s.clock())
	if err != nil {
		return time.Time{}, false, storeError("oldest expired", err)
	}
	return at, ok, nil
}
