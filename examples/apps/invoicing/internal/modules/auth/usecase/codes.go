package usecase

import (
	"context"
	"errors"
	"time"

	authlib "gorbital.dev/modules/auth"

	authdomain "example.com/invoicing/internal/modules/auth/domain"
)

var errThrottled = errors.New("auth: code requested too recently")

// issueCode ends earlier codes of purpose and stores a new one, returning the
// code to email. With minInterval, it returns errThrottled when the previous
// code is newer.
func (s *Service) issueCode(ctx context.Context, tx Store, userID, purpose string, ttl, minInterval time.Duration) (string, error) {
	now := s.now()
	if minInterval > 0 {
		latest, found, err := tx.SelectLatestCodeTime(ctx, userID, purpose)
		if err != nil {
			return "", err
		}
		if found && now.Sub(latest) < minInterval {
			return "", errThrottled
		}
	}
	if err := tx.ConsumeCodes(ctx, userID, purpose, now); err != nil {
		return "", err
	}
	id, code := authlib.NewID("cod"), authlib.NewCode()
	err := tx.InsertCode(ctx, authdomain.Code{
		ID: id, UserID: userID, Purpose: purpose, Hash: authlib.HashCode(id, code),
		MaxAttempts: authlib.CodeMaxAttempts, ExpiresAt: now.Add(ttl), CreatedAt: now,
	})
	if err != nil {
		return "", err
	}
	return code, nil
}

// checkCode compares code with the account's newest usable code of purpose,
// consuming it on a match and counting a failed attempt otherwise.
func (s *Service) checkCode(ctx context.Context, tx Store, userID, purpose, code string) (bool, error) {
	now := s.now()
	c, found, err := tx.SelectActiveCode(ctx, userID, purpose, now)
	if err != nil || !found {
		return false, err
	}
	if authlib.CodeMatches(c.ID, code, c.Hash) {
		return true, tx.ConsumeCode(ctx, c.ID, now)
	}
	return false, tx.FailCode(ctx, c.ID, now)
}
