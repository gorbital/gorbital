package usecase

import (
	"context"
	"time"
)

// Ping reports that the API is reachable.
func (s *Service) Ping(ctx context.Context) string { return s.message.Get(ctx) }

// ServerTime returns the server's time and true when the caller gets it in
// ping replies: an example of a feature rolled out with a flag.
func (s *Service) ServerTime(ctx context.Context) (time.Time, bool) {
	if !s.serverTime.Get(ctx) {
		return time.Time{}, false
	}
	return time.Now().UTC(), true
}
