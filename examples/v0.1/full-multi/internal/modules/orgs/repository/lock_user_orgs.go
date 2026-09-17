package repository

import "context"

// The lock key is namespaced, so it can't collide with other advisory locks
// on the same user ID.
const lockUserOrgsSQL = `SELECT pg_advisory_xact_lock(hashtextextended('orgs.owned:' || $1::text, 0))`

// LockUserOrgs takes a transaction-scoped advisory lock on userID's owned
// organisations, so two requests creating or restoring organisations for
// the same user check the limit one after the other.
func (s *Store) LockUserOrgs(ctx context.Context, userID string) error {
	_, err := s.db.Exec(ctx, lockUserOrgsSQL, userID)
	return err
}
