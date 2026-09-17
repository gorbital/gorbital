package usecase

import (
	"context"

	opsdomain "gorbital.dev/gorbital/opshttp/internal/domain"
)

// ListSignInMethods returns each sign-in method, whether it is configured,
// and the environment variables that turn the others on (ADR-0045). It never
// returns configuration values.
func (s *Service) ListSignInMethods(ctx context.Context) ([]opsdomain.SignInMethod, error) {
	if err := authorize(ctx, opsdomain.PermAuthRead); err != nil {
		return nil, err
	}
	if s.signInMethods == nil {
		return nil, nil
	}
	return s.signInMethods(), nil
}
