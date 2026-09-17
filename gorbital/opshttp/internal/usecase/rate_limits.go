package usecase

import (
	"context"
	"errors"
	"strings"

	"gorbital.dev/audit"

	opsdomain "gorbital.dev/gorbital/opshttp/internal/domain"
)

// RateLimitAdmin is what the app knows about its rate limiters (ADR-0070).
type RateLimitAdmin interface {
	// Limiters describes every limiter: its name and what its keys are.
	Limiters() []opsdomain.RateLimiter
	// Reset forgets the budget of key under the limiter named name and
	// reports whether one was kept.
	Reset(ctx context.Context, name, key string) (bool, error)
}

// ErrUnknownRateLimiter reports a limiter name the app doesn't have.
var ErrUnknownRateLimiter = errors.New("unknown rate limiter")

// ListRateLimits lists the app's rate limiters.
func (s *Service) ListRateLimits(ctx context.Context) ([]opsdomain.RateLimiter, error) {
	if err := authorize(ctx, opsdomain.PermAuthRead); err != nil {
		return nil, err
	}
	if s.rateLimits == nil {
		return []opsdomain.RateLimiter{}, nil
	}
	return s.rateLimits.Limiters(), nil
}

// ResetRateLimit forgets a key's budget under a limiter, so a client the
// limit refused is allowed again. It returns ErrUnknownRateLimiter for a
// name the app doesn't have.
func (s *Service) ResetRateLimit(ctx context.Context, name, key string) (bool, error) {
	if err := authorize(ctx, opsdomain.PermAuthWrite); err != nil {
		return false, err
	}
	key = strings.TrimSpace(key)
	if s.rateLimits == nil || key == "" {
		return false, ErrUnknownRateLimiter
	}
	known := false
	for _, l := range s.rateLimits.Limiters() {
		if l.Name == name {
			known = true
		}
	}
	if !known {
		return false, ErrUnknownRateLimiter
	}
	reset, err := s.rateLimits.Reset(ctx, name, key)
	if err != nil {
		return false, err
	}
	// The key may be an address or an email: the audit event names the
	// limiter only.
	err = s.audit.Record(ctx, audit.Event{
		Action: "ops.rate_limit.reset", ResourceType: "rate_limiter", ResourceID: name,
		Outcome: audit.OutcomeSuccess, Metadata: map[string]any{"reset": reset},
	})
	return reset, err
}
