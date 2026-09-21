package usecase

import (
	"context"

	"gorbital.dev/modules/flags"
	orgslib "gorbital.dev/modules/orgs"
)

// FlagsStore evaluates the feature flags clients may read (ADR-0057).
// *flags.Store implements it.
type FlagsStore interface {
	ClientFlags(ctx context.Context) []flags.Evaluation
}

var _ FlagsStore = (*flags.Store)(nil)

// Flags returns whether each feature flag clients may read is on for the
// signed-in member acting in the organisation, by key. The organisation's
// allow and deny lists apply, and it is the subject of percentage rollouts,
// so every member gets the same answer unless a user list names them.
func (s *Service) Flags(ctx context.Context, orgID orgslib.ID) (map[string]bool, error) {
	ctx, _, err := orgslib.RequireMember(ctx, s.store, s.catalog, orgID, PermOrgRead)
	if err != nil {
		return nil, storeError("list flags", err)
	}
	out := map[string]bool{}
	if s.flags == nil {
		return out, nil
	}
	for _, e := range s.flags.ClientFlags(ctx) {
		out[e.Key] = e.Enabled
	}
	return out, nil
}
