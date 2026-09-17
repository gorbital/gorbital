package usecase

import (
	"context"

	"gorbital.dev/modules/flags"

	opsdomain "gorbital.dev/gorbital/opshttp/internal/domain"
)

// FlagsStore is the feature flags store (ADR-0057). *flags.Store implements
// it.
type FlagsStore interface {
	List() []flags.View
	Get(key string) (flags.View, error)
	Set(ctx context.Context, key string, state flags.State, change flags.Change) (flags.View, error)
	Reset(ctx context.Context, key string, change flags.Change) (flags.View, error)
	History(ctx context.Context, key string, before int64, limit int) ([]flags.HistoryEntry, error)
}

var _ FlagsStore = (*flags.Store)(nil)

// ListFlags returns every feature flag, or those in group.
func (s *Service) ListFlags(ctx context.Context, group string) ([]flags.View, error) {
	if err := authorize(ctx, opsdomain.PermFlagsRead); err != nil {
		return nil, err
	}
	all := s.flags.List()
	if group == "" {
		return all, nil
	}
	filtered := all[:0]
	for _, v := range all {
		if v.Group == group {
			filtered = append(filtered, v)
		}
	}
	return filtered, nil
}

// GetFlag returns one feature flag.
func (s *Service) GetFlag(ctx context.Context, key string) (flags.View, error) {
	if err := authorize(ctx, opsdomain.PermFlagsRead); err != nil {
		return flags.View{}, err
	}
	return s.flags.Get(key)
}

// SetFlag changes a feature flag's state. Every change needs a reason.
func (s *Service) SetFlag(ctx context.Context, key string, state flags.State, change flags.Change) (flags.View, error) {
	if err := authorize(ctx, opsdomain.PermFlagsWrite); err != nil {
		return flags.View{}, err
	}
	return s.flags.Set(ctx, key, state, change)
}

// ResetFlag returns a feature flag to its declared state.
func (s *Service) ResetFlag(ctx context.Context, key string, change flags.Change) (flags.View, error) {
	if err := authorize(ctx, opsdomain.PermFlagsWrite); err != nil {
		return flags.View{}, err
	}
	return s.flags.Reset(ctx, key, change)
}

// FlagHistory returns a feature flag's changes, newest first.
func (s *Service) FlagHistory(ctx context.Context, key string, before int64, limit int) ([]flags.HistoryEntry, error) {
	if err := authorize(ctx, opsdomain.PermFlagsRead); err != nil {
		return nil, err
	}
	return s.flags.History(ctx, key, before, limit)
}
