package usecase

import (
	"context"

	"example.com/shelfie/internal/modules/phonelogin/domain"
)

// docs:start verify-sign-in-code

// VerifySignInCode checks a sign-in code texted to phone and returns the
// account that confirmed the number. Each code works once, for 10 minutes
// and 5 guesses; a wrong, used or expired code and a number without one all
// return ErrInvalidCode. The number must still be confirmed by that account.
func (s *Service) VerifySignInCode(ctx context.Context, phone, code string) (string, error) {
	phone, err := domain.NormalizePhone(phone)
	if err != nil {
		return "", domain.ErrInvalidCode
	}
	userID, err := s.checkCode(ctx, phone, domain.PurposeSignIn, code)
	if err != nil {
		return "", err
	}
	current, found, err := s.store.SelectConfirmedUser(ctx, phone)
	switch {
	case err != nil:
		return "", err
	case !found || current != userID:
		return "", domain.ErrInvalidCode // the number moved since the code was sent
	}
	return userID, nil
}

// docs:end verify-sign-in-code
