package usecase

import (
	"context"
	"errors"
	"slices"
	"time"

	"gorbital.dev/actor"
	"gorbital.dev/audit"
	authlib "gorbital.dev/modules/auth"
	"gorbital.dev/modules/auth/social"

	authdomain "example.com/invoicing/internal/modules/auth/domain"
)

const (
	// revocationBatch is how many provider tokens one job run revokes; with
	// the provider client's 10-second timeout it finishes within the lease.
	revocationBatch = 20
	// revocationLease is how long a claimed revocation is hidden from other
	// runs.
	revocationLease = 10 * time.Minute
)

// ListIdentities returns the Google, Apple and GitHub accounts linked to the
// signed-in user, oldest first.
func (s *Service) ListIdentities(ctx context.Context) ([]authdomain.Identity, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	identities, err := s.store.SelectIdentities(ctx, p.UserID)
	if err != nil {
		return nil, dbError("list sign-in methods", err)
	}
	return identities, nil
}

// RemoveIdentity unlinks one of the signed-in user's Google, Apple or GitHub
// accounts. It checks the user as confirmUser does, refuses to remove the
// account's last way to sign in (no password, passkey or other identity
// left), and queues Apple's token for revocation. It returns
// ErrInvalidCredentials, ErrInvalidMFA, ErrIdentityNotFound,
// ErrLastSignInMethod or a *RateLimitError.
func (s *Service) RemoveIdentity(ctx context.Context, id, password string) error {
	p, err := s.reauthPrincipal(ctx)
	if err != nil {
		return err
	}
	var (
		removed authdomain.Identity
		to      string
		state   error
	)
	err = s.store.InTx(ctx, func(tx Store) error {
		u, err := tx.SelectUserByID(ctx, p.UserID, true)
		if err != nil {
			return err // ErrUserNotFound is handled below
		}
		if state, err = s.confirmUser(ctx, tx, p, u, password); err != nil || state != nil {
			return err
		}
		identities, err := tx.SelectIdentities(ctx, u.ID)
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(identities, func(i authdomain.Identity) bool { return i.ID == id }) {
			state = authdomain.ErrIdentityNotFound
			return nil
		}
		passkeys, err := tx.CountPasskeys(ctx, u.ID)
		if err != nil {
			return err
		}
		if !u.HasPassword() && passkeys == 0 && len(identities) == 1 {
			state = authdomain.ErrLastSignInMethod
			return nil
		}
		to = u.Email
		if removed, _, err = tx.DeleteIdentity(ctx, id, u.ID); err != nil {
			return err
		}
		return s.queueRevocations(ctx, tx, removed)
	})
	switch {
	case errors.Is(err, authdomain.ErrUserNotFound):
		return authlib.ErrUnauthenticated
	case err != nil:
		return dbError("remove sign-in method", err)
	case state != nil:
		return s.reauthFailed(ctx, p.UserID, state)
	}
	s.sent(ctx, "sign_in_method_removed", s.emails.SendSignInMethodRemoved(ctx, to, providerName(removed.Provider)))
	e := userEvent("auth.identity.unlinked", p.UserID, authlib.ClientInfoFromContext(ctx))
	e.Metadata = map[string]any{"provider": removed.Provider, "identity_id": removed.ID}
	s.audit(ctx, e)
	return nil
}

// HandleAppleNotification acts on a server-to-server notification Apple
// posts: consent-revoked and account-delete unlink the Apple identity and,
// when the account has no other way to sign in, end its sessions; email
// changes are recorded. Unknown people are ignored. Apple has already
// revoked the tokens, so none are queued. A notification older than
// social.MaxNotificationAge is refused, one seen before is ignored, and an
// unlink event from before the identity was linked leaves it linked, so a
// leaked payload can't be replayed (security review AUTH-M-3). It returns
// ErrSocialUnavailable or ErrInvalidSocialToken.
func (s *Service) HandleAppleNotification(ctx context.Context, payload string) error {
	p := s.providers[social.Apple]
	if p == nil {
		return authdomain.ErrSocialUnavailable
	}
	n, err := p.AppleNotification(ctx, payload)
	if err != nil {
		s.logger.WarnContext(ctx, "Apple notification refused", "err", err)
		return authdomain.ErrInvalidSocialToken
	}
	unlink := n.Type == social.NotificationConsentRevoked || n.Type == social.NotificationAccountDelete
	var (
		userID                  string
		ended, replayed, before bool
	)
	now := s.now()
	err = s.store.InTx(ctx, func(tx Store) error {
		first, err := tx.RecordAppleNotification(ctx, authdomain.SocialNonce{
			ID: authlib.NewID("snc"), TokenHash: authlib.HashToken("apple-notification:" + n.ID), Provider: social.Apple,
			ExpiresAt: n.IssuedAt.Add(social.MaxNotificationAge + time.Minute), CreatedAt: now,
		})
		if err != nil || !first {
			replayed = !first
			return err
		}
		identity, found, err := tx.SelectIdentity(ctx, social.Apple, n.Subject, unlink)
		if err != nil || !found {
			return err
		}
		userID = identity.UserID
		if !unlink {
			return nil
		}
		// Clocks disagree slightly; an event a minute older than the link is
		// about an earlier one.
		if !n.At.IsZero() && n.At.Before(identity.CreatedAt.Add(-time.Minute)) {
			before = true
			return nil
		}
		if _, _, err := tx.DeleteIdentity(ctx, identity.ID, identity.UserID); err != nil {
			return err
		}
		u, err := tx.SelectUserByID(ctx, identity.UserID, true)
		if errors.Is(err, authdomain.ErrUserNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		passkeys, err := tx.CountPasskeys(ctx, u.ID)
		if err != nil {
			return err
		}
		others, err := tx.CountIdentities(ctx, u.ID)
		if err != nil || u.HasPassword() || passkeys > 0 || others > 0 {
			return err
		}
		ended = true
		_, err = tx.RevokeUserSessions(ctx, u.ID, "", now, "apple_"+n.Type)
		return err
	})
	if err != nil {
		return dbError("handle Apple notification", err)
	}
	e := userEvent("auth.identity.apple_notification", userID, authlib.ClientInfo{})
	e.ActorKind, e.ActorID = "system", "apple"
	e.Metadata = map[string]any{"type": n.Type, "known": userID != "", "sessions_ended": ended}
	if replayed {
		e.Metadata["replayed"] = true
	}
	if before {
		e.Metadata["before_link"] = true
	}
	s.audit(ctx, e)
	return nil
}

// queueRevocations queues the provider refresh tokens of removed identities
// for the auth_revoke_tokens job, in the transaction that removed them, so
// no token is lost and the request never waits on the provider.
func (s *Service) queueRevocations(ctx context.Context, tx Store, identities ...authdomain.Identity) error {
	now := s.now()
	for _, i := range identities {
		if len(i.RefreshTokenCiphertext) == 0 {
			continue
		}
		err := tx.InsertTokenRevocation(ctx, authdomain.TokenRevocation{
			ID: authlib.NewID("rvk"), Provider: i.Provider, Subject: i.Subject, ClientID: i.RefreshClientID,
			KeyID: i.RefreshKeyID, TokenCiphertext: i.RefreshTokenCiphertext, NextAttemptAt: now, CreatedAt: now,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// RevokeProviderTokens revokes queued provider tokens that are due, for the
// auth_revoke_tokens job. A failed revocation is retried with
// RevocationBackoff and abandoned, with an audit event, after
// MaxRevocationAttempts. Only database errors are returned.
func (s *Service) RevokeProviderTokens(ctx context.Context) (authdomain.RevocationResult, error) {
	var res authdomain.RevocationResult
	now := s.now()
	due, err := s.store.ClaimTokenRevocations(ctx, now, now.Add(revocationLease), revocationBatch)
	if err != nil {
		return res, dbError("claim token revocations", err)
	}
	for _, r := range due {
		revokeErr := s.revokeToken(ctx, r)
		attempts := r.Attempts + 1
		switch {
		case revokeErr == nil:
			if err := s.store.DeleteTokenRevocation(ctx, r.ID); err != nil {
				return res, dbError("finish token revocation", err)
			}
			res.Revoked++
		case attempts >= authdomain.MaxRevocationAttempts:
			if err := s.store.DeleteTokenRevocation(ctx, r.ID); err != nil {
				return res, dbError("abandon token revocation", err)
			}
			res.Abandoned++
			s.logger.ErrorContext(ctx, "gave up revoking a provider token", "provider", r.Provider, "revocation_id", r.ID, "attempts", attempts, "err", revokeErr)
			s.audit(ctx, auditEvent{
				Action: "auth.identity.revocation_abandoned", Outcome: audit.OutcomeFailure, ActorKind: actor.KindSystem, ActorID: "auth_revoke_tokens",
				Metadata: map[string]any{"provider": r.Provider, "revocation_id": r.ID, "attempts": attempts},
			})
		default:
			if err := s.store.RetryTokenRevocation(ctx, r.ID, attempts, s.now().Add(authdomain.RevocationBackoff(attempts)), truncate(revokeErr.Error(), 500)); err != nil {
				return res, dbError("retry token revocation", err)
			}
			res.Retrying++
			s.logger.WarnContext(ctx, "revoke a provider token; will retry", "provider", r.Provider, "revocation_id", r.ID, "attempts", attempts, "err", revokeErr)
		}
	}
	return res, nil
}

// revokeToken decrypts a queued token and revokes it at its provider.
func (s *Service) revokeToken(ctx context.Context, r authdomain.TokenRevocation) error {
	p := s.providers[r.Provider]
	if p == nil || s.keyring == nil {
		return errors.New("the provider or AUTH_ENCRYPTION_KEYS isn't configured")
	}
	token, err := s.keyring.Decrypt(r.KeyID, r.TokenCiphertext, identityAAD(r.Provider, r.Subject))
	if err != nil {
		return err
	}
	return p.Revoke(ctx, string(token), r.ClientID)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
