package usecase

import "context"

// DeleteBook removes one of the signed-in reader's books.
func (s *Service) DeleteBook(ctx context.Context, id string) error {
	reader, err := readerID(ctx)
	if err != nil {
		return err
	}
	if err := s.store.DeleteBook(ctx, reader, id); err != nil {
		return storeError("delete", err)
	}
	s.audit(ctx, ActionDeleted, id)
	return nil
}
