// Package usecase holds the profiles module's operations, one file each.
package usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorbital.dev/actor"

	"example.com/shelfie/internal/modules/profiles/domain"
)

// Permissions of the profile routes, held by every reader through the user
// role. Permission names are public API.
const (
	PermRead  = "profiles.profile.read"
	PermWrite = "profiles.profile.write"
)

// Store reads and writes profiles; repository.Store implements it with SQL.
type Store interface {
	// SelectProfile returns a reader's profile, or ErrProfileIncomplete.
	SelectProfile(ctx context.Context, userID string) (domain.Profile, error)
	// UpsertProfile creates or changes a reader's profile.
	UpsertProfile(ctx context.Context, p domain.Profile) (domain.Profile, error)
}

// Service runs the profile use cases. It is safe for concurrent use.
type Service struct {
	store Store
	now   func() time.Time
}

// NewService returns a Service on store, which may be bound to a
// transaction.
func NewService(store Store) *Service {
	return &Service{store: store, now: time.Now}
}

func (s *Service) clock() time.Time { return s.now().UTC().Truncate(time.Microsecond) }

// readerID returns the signed-in reader.
func readerID(ctx context.Context) (string, error) {
	a, ok := actor.From(ctx)
	if !ok || a.Kind != actor.KindUser || a.ID == "" {
		return "", domain.ErrUnauthenticated
	}
	return a.ID, nil
}

// storeError keeps the module's errors and hides driver errors.
func storeError(op string, err error) error {
	if errors.Is(err, domain.ErrProfileIncomplete) {
		return err
	}
	return fmt.Errorf("profiles: %s: %v", op, err) //nolint:errorlint // driver errors aren't API
}
