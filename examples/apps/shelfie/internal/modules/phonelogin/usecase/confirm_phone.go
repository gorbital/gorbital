package usecase

import (
	"context"

	"example.com/shelfie/internal/modules/phonelogin/domain"
)

// ConfirmPhone confirms the signed-in reader's number with the code
// SetPhone texted to it. It returns ErrInvalidCode or ErrPhoneTaken.
func (s *Service) ConfirmPhone(ctx context.Context, phone, code string) error {
	reader, err := readerID(ctx)
	if err != nil {
		return err
	}
	if phone, err = domain.NormalizePhone(phone); err != nil {
		return err
	}
	owner, err := s.checkCode(ctx, phone, domain.PurposeConfirm, code)
	if err != nil {
		return err
	}
	if owner != reader {
		return domain.ErrInvalidCode // another reader's code for the same number
	}
	return s.store.ConfirmPhone(ctx, reader, phone, s.clock())
}
