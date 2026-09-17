package delivery

import (
	"time"

	"example.com/admin-tool/internal/modules/announcements/domain"
)

// AnnouncementResponse is an announcement as the API returns it.
type AnnouncementResponse struct {
	ID          string    `json:"id" example:"ann_mfrggzdfmztwq2lkmfrggzdfmy"`
	Title       string    `json:"title" example:"Scheduled maintenance"`
	Body        string    `json:"body" example:"The app is unavailable on Saturday from 02:00 to 03:00 UTC."`
	PublishedBy string    `json:"published_by" example:"usr_mfrggzdfmztwq2lk"`
	StartsAt    time.Time `json:"starts_at"`
	EndsAt      time.Time `json:"ends_at"`
}

func toResponse(a domain.Announcement) AnnouncementResponse {
	return AnnouncementResponse{
		ID: a.ID, Title: a.Title, Body: a.Body, PublishedBy: a.PublishedBy, StartsAt: a.StartsAt, EndsAt: a.EndsAt,
	}
}
