package usecase

import (
	"context"

	"example.com/shelfie/internal/modules/phonelogin/domain"
)

// SetPhone sets the signed-in reader's number and texts it a code to
// confirm it with ConfirmPhone. The number signs in only once confirmed.
func (s *Service) SetPhone(ctx context.Context, phone string) error {
	reader, err := readerID(ctx)
	if err != nil {
		return err
	}
	if phone, err = domain.NormalizePhone(phone); err != nil {
		return err
	}
	if err := s.store.UpsertPhone(ctx, reader, phone, s.clock()); err != nil {
		return err
	}
	_, err = s.issueCode(ctx, reader, phone, domain.PurposeConfirm)
	return err
}
