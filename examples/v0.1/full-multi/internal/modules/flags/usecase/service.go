// Package usecase holds the feature flags module's application logic.
package usecase

import (
	"context"

	"gorbital.dev/actor"
	"gorbital.dev/modules/flags"

	flagsdomain "example.com/acme-api/internal/modules/flags/domain"
)

// PermFlagsRead lets a caller read the client flags. The user role grants it
// to every user; an API key needs it in its scopes (ADR-0058).
const PermFlagsRead = "flags.flag.read"

// FlagsStore evaluates the flags clients may read. *flags.Store implements
// it.
type FlagsStore interface {
	ClientFlags(ctx context.Context) []flags.Evaluation
}

var _ FlagsStore = (*flags.Store)(nil)

// Service runs the feature flags use cases.
type Service struct {
	store FlagsStore
}

// NewService returns a Service.
func NewService(store FlagsStore) *Service {
	return &Service{store: store}
}

// ClientFlags returns whether each flag declared flags.Client() is on for
// the signed-in caller, by key. The caller is the subject of percentage
// rollouts. It needs PermFlagsRead.
func (s *Service) ClientFlags(ctx context.Context) (map[string]bool, error) {
	if a, ok := actor.From(ctx); !ok || a.Kind == actor.KindAnonymous || a.ID == "" {
		return nil, flagsdomain.ErrUnauthenticated
	}
	if err := actor.Require(ctx, PermFlagsRead); err != nil {
		return nil, flagsdomain.ErrForbidden
	}
	out := map[string]bool{}
	for _, e := range s.store.ClientFlags(ctx) {
		out[e.Key] = e.Enabled
	}
	return out, nil
}
