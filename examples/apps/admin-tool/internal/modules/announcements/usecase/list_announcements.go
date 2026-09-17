package usecase

import (
	"context"

	"example.com/admin-tool/internal/modules/announcements/domain"
)

// ListAnnouncements returns the active announcements, newest first, up to
// MaxList. Anyone may read them.
func (s *Service) ListAnnouncements(ctx context.Context) ([]domain.Announcement, error) {
	list, err := s.store.SelectActive(ctx, s.clock(), MaxList)
	if err != nil {
		return nil, storeError("list", err)
	}
	return list, nil
}
