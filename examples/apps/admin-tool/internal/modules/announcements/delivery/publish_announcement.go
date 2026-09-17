package delivery

import (
	"context"
	"time"

	"example.com/admin-tool/internal/modules/announcements/domain"
)

type publishAnnouncementInput struct {
	Body struct {
		Title  string    `json:"title" minLength:"1" maxLength:"200"`
		Body   string    `json:"body" minLength:"1" maxLength:"5000"`
		EndsAt time.Time `json:"ends_at" doc:"When customers stop seeing it; in the future"`
	}
}

type announcementOutput struct {
	Body AnnouncementResponse
}

func (h handlers) publishAnnouncement(ctx context.Context, in *publishAnnouncementInput) (*announcementOutput, error) {
	a, err := h.svc.PublishAnnouncement(ctx, domain.Fields{Title: in.Body.Title, Body: in.Body.Body, EndsAt: in.Body.EndsAt})
	if err != nil {
		return nil, err
	}
	return &announcementOutput{Body: toResponse(a)}, nil
}
