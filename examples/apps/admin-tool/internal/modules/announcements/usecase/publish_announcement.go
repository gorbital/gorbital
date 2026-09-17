package usecase

import (
	"context"

	"example.com/admin-tool/internal/modules/announcements/domain"
)

// docs:start publish

// PublishAnnouncement publishes an announcement from now until f.EndsAt,
// as the signed-in staff member.
func (s *Service) PublishAnnouncement(ctx context.Context, f domain.Fields) (domain.Announcement, error) {
	publisher, err := publisherID(ctx)
	if err != nil {
		return domain.Announcement{}, err
	}
	now := s.clock()
	a, err := domain.NewAnnouncement(s.newID(), publisher, f, now)
	if err != nil {
		return domain.Announcement{}, err
	}
	if err := s.checkActive(ctx, a); err != nil {
		return domain.Announcement{}, err
	}
	published, err := s.store.InsertAnnouncement(ctx, a)
	if err != nil {
		return domain.Announcement{}, storeError("publish", err)
	}
	s.audit(ctx, ActionPublished, published.ID, nil)
	return published, nil
}

// docs:end publish

// docs:start check-active

// checkActive returns ErrTooManyAnnouncements when as many announcements are
// active as announcements.max_active allows now. Two published at the same
// moment can both pass: the limit keeps the banner readable, it isn't a
// quota to enforce exactly.
func (s *Service) checkActive(ctx context.Context, a domain.Announcement) error {
	if s.maxActive == nil {
		return nil
	}
	n, err := s.store.CountActive(ctx, a.StartsAt)
	if err != nil {
		return storeError("count", err)
	}
	if n >= s.maxActive.Get(ctx) {
		return domain.ErrTooManyAnnouncements
	}
	return nil
}

// docs:end check-active
