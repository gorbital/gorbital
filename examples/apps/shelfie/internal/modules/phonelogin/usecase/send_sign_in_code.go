package usecase

import (
	"context"
	"time"

	"example.com/shelfie/internal/modules/phonelogin/domain"
)

// docs:start send-sign-in-code

// SendSignInCode texts a sign-in code to phone when an account confirmed it,
// at most once a minute. Whether or not one did, it returns nil after the
// same minimum time, so neither the answer nor its timing tells a caller
// whose number it is. A malformed number returns ErrInvalidPhone, which
// says nothing about accounts.
func (s *Service) SendSignInCode(ctx context.Context, phone string) error {
	phone, err := domain.NormalizePhone(phone)
	if err != nil {
		return err
	}
	defer s.pad(ctx, time.Now())
	userID, found, err := s.store.SelectConfirmedUser(ctx, phone)
	if err != nil || !found {
		return err
	}
	_, err = s.issueCode(ctx, userID, phone, domain.PurposeSignIn)
	return err
}

// docs:end send-sign-in-code

// pad waits until minResponse has passed since start.
func (s *Service) pad(ctx context.Context, start time.Time) {
	wait := s.minResponse - time.Since(start)
	if wait <= 0 {
		return
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
	}
}
