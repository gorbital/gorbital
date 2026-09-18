package usecase

import (
	"context"

	"example.com/plateful/internal/modules/orders/domain"
)

// beforeMove runs inside the transaction, with the order locked and before
// it changes, for the rules one particular move has. It may change the order
// it is given, which is then what is saved.
type beforeMove func(ctx context.Context, tx Tx, o domain.Order) error

// docs:start move-order

// move is the one path every status change of a restaurant's own order
// takes: lock the row, let the extra rule of this particular move speak,
// ask the domain whether the move is legal, save, and record what happened.
//
// Writing it once is what keeps the state machine honest. Six handlers each
// doing their own version is how an order ends up delivered twice.
func (s *Service) move(ctx context.Context, orgID, id string, to domain.Status, reason, action string, before beforeMove) (domain.Order, error) {
	if _, err := memberID(ctx, orgID); err != nil {
		return domain.Order{}, err
	}
	var moved domain.Order
	err := s.tx.InTx(ctx, func(tx Tx) error {
		current, err := tx.SelectOrgOrder(ctx, orgID, id, true)
		if err != nil {
			return err
		}
		if before != nil {
			if err := before(ctx, tx, current); err != nil {
				return err
			}
			// before may have assigned a courier; read the row again so the
			// save carries it.
			if current, err = tx.SelectOrgOrder(ctx, orgID, id, true); err != nil {
				return err
			}
		}
		next, err := current.MoveTo(to, reason, s.clock())
		if err != nil {
			return err
		}
		moved, err = tx.UpdateOrder(ctx, next)
		return err
	})
	if err != nil {
		return domain.Order{}, storeError("move", err)
	}
	s.audit(ctx, action, moved.ID, map[string]any{"status": string(moved.Status)})
	return moved, nil
}

// docs:end move-order
