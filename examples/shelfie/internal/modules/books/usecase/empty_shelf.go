package usecase

import "context"

// docs:start empty-shelf

// EmptyShelf removes every book on the signed-in reader's shelf and returns
// how many it removed. Nothing keeps a copy, which is why the route asks for
// a recent sign-in (delivery/routes.go).
func (s *Service) EmptyShelf(ctx context.Context) (int, error) {
	reader, err := readerID(ctx)
	if err != nil {
		return 0, err
	}
	removed, err := s.store.DeleteBooks(ctx, reader)
	if err != nil {
		return 0, storeError("empty shelf", err)
	}
	s.auditResource(ctx, ActionShelfEmptied, "shelf", reader)
	return removed, nil
}

// docs:end empty-shelf
