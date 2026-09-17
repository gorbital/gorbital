package usecase

import (
	"context"
	"strings"

	"example.com/admin-tool/internal/modules/announcements/domain"
)

// docs:start withdraw

// WithdrawAnnouncement removes an announcement customers can still see,
// because it was wrong or the maintenance window moved. The reason is kept
// in the audit event, not in the row, which is gone: reviewing a withdrawal
// later means reading /ops/audit.
func (s *Service) WithdrawAnnouncement(ctx context.Context, id, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return domain.ErrReasonRequired
	}
	withdrawn, err := s.store.DeleteAnnouncement(ctx, id)
	if err != nil {
		return storeError("withdraw", err)
	}
	if !withdrawn {
		return domain.ErrAnnouncementNotFound
	}
	s.audit(ctx, ActionWithdrawn, id, map[string]any{"reason": reason})
	return nil
}

// docs:end withdraw
