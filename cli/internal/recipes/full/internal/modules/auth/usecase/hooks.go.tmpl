package usecase

import (
	"context"
)

// AccountHooks let other modules take part in creating and deleting
// accounts without the auth module importing them. The composition root
// sets them in Config; a multi-tenant app uses them for personal workspaces
// and organisation ownership (ADR-0048).
//
// Hooks don't share the auth module's transaction: each module keeps its own
// tables and transactions, so hooks must be safe to run again and repair
// what an earlier failure left behind.
type AccountHooks interface {
	// AccountCreated runs after an account is created: registration, a first
	// Google, Apple or GitHub sign-in, or CreateUser. Its error is logged, not
	// returned, because the account exists either way.
	AccountCreated(ctx context.Context, userID string) error
	// CheckAccountDeletion runs before the signed-in user's account is
	// deleted. An error stops the deletion and is returned as is.
	CheckAccountDeletion(ctx context.Context, userID string) error
	// AccountDeleted runs after an account is deleted. Its error is logged.
	AccountDeleted(ctx context.Context, userID string) error
}

// accountCreated runs the AccountCreated hook, if any, and logs its error.
func (s *Service) accountCreated(ctx context.Context, userID string) {
	if s.hooks == nil {
		return
	}
	if err := s.hooks.AccountCreated(context.WithoutCancel(ctx), userID); err != nil {
		s.logger.ErrorContext(ctx, "account created hook", "user_id", userID, "err", err)
	}
}

// checkAccountDeletion runs the CheckAccountDeletion hook, if any.
func (s *Service) checkAccountDeletion(ctx context.Context, userID string) error {
	if s.hooks == nil {
		return nil
	}
	return s.hooks.CheckAccountDeletion(ctx, userID)
}

// accountDeleted runs the AccountDeleted hook, if any, and logs its error.
func (s *Service) accountDeleted(ctx context.Context, userID string) {
	if s.hooks == nil {
		return
	}
	if err := s.hooks.AccountDeleted(context.WithoutCancel(ctx), userID); err != nil {
		s.logger.ErrorContext(ctx, "account deleted hook", "user_id", userID, "err", err)
	}
}
