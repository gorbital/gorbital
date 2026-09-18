package usecase

import (
	"context"
	"errors"
	"time"

	authlib "gorbital.dev/modules/auth"

	authdomain "example.com/plateful/internal/modules/auth/domain"
)

// Register creates an account and emails a verification code. It returns
// only auth.ErrInvalidEmail or an *auth.PasswordError for bad input (or
// auth.ErrHasherBusy), and the result, like its timing, never reveals
// whether the address has an account:
//
//   - a new address gets an account and a code;
//   - an address with an unverified account gets a new code, at most once a
//     minute. Nobody proved they own the address yet, so the account keeps
//     its password only when the same password is given again; otherwise
//     it loses it, and whoever verifies the address sets one with password
//     reset. Neither the first nor the last registrant can choose the
//     password of an account its owner then verifies (security review
//     AUTH-S-1);
//   - an address with a verified account gets an "account exists" notice, at
//     most once a minute.
func (s *Service) Register(ctx context.Context, email, password string) error {
	return s.RegisterWithFields(ctx, email, password, nil)
}

// RegisterWithFields is Register with the app's extra registration fields
// (authhttp's RegisterFields), which the OnRegister hook receives in the
// transaction that creates the account. A hook's error, refusal or not,
// rolls the account back and is logged, and the result is still nil: a
// response that differed would tell whoever registers that the address had
// no account, because hooks run only for new accounts. Refuse bad fields
// with their validation instead, which runs for every request.
func (s *Service) RegisterWithFields(ctx context.Context, email, password string, fields any) error {
	email, normalized, err := authlib.NormalizeEmail(email)
	if err != nil {
		return err
	}
	if err := authlib.ValidatePassword(ctx, password, s.checker); err != nil {
		return err
	}
	defer s.padResponse(ctx, time.Now())
	// Hash in every case, so each outcome takes the same work.
	hash, err := s.hasher.HashContext(ctx, password)
	if err != nil {
		return err
	}
	client := authlib.ClientInfoFromContext(ctx)
	ttl := authlib.VerificationCodeLimits.Clamp(s.verificationCode.Get(ctx))
	now := s.now()

	var (
		userID, to, code string
		created, exists  bool
		hookErr          error
	)
	err = s.store.InTx(ctx, func(tx Store) error {
		u, err := tx.SelectUserByEmail(ctx, normalized, true)
		switch {
		case errors.Is(err, authdomain.ErrUserNotFound):
			u, err = tx.InsertUser(ctx, authdomain.User{
				ID: authlib.NewID("usr"), Email: email, NormalizedEmail: normalized, PasswordHash: hash, CreatedAt: now,
			})
			if err != nil {
				return err
			}
			created = true
			if err := s.onRegister(ctx, tx, NewAccount{User: u, Method: authdomain.MethodPassword, Client: client}, fields); err != nil {
				hookErr = err
				return err
			}
		case err != nil:
			return err
		case u.EmailVerified():
			exists, to = true, u.Email
			return nil
		case u.HasPassword():
			same, _, err := s.hasher.VerifyContext(ctx, password, u.PasswordHash)
			if err != nil {
				return err
			}
			if !same {
				if err := tx.RemovePassword(ctx, u.ID, now); err != nil {
					return err
				}
			}
		}
		userID, to = u.ID, u.Email
		interval := authlib.CodeResendInterval
		if created {
			interval = 0
		}
		code, err = s.issueCode(ctx, tx, userID, authdomain.PurposeVerifyEmail, ttl, interval)
		if errors.Is(err, errThrottled) {
			return nil // commit a removed password
		}
		return err
	})
	switch {
	case hookErr != nil:
		s.logger.ErrorContext(ctx, "registration refused by the app's OnRegister hook; the account was rolled back", "err", hookErr)
		return nil
	case errors.Is(err, authdomain.ErrEmailTaken):
		return nil // registered at the same moment by another request
	case err != nil:
		return dbError("register", err)
	case exists:
		if ok, _ := s.allow(ctx, s.noticeLimiter, normalized); ok {
			s.sent(ctx, "account_exists", s.emails.SendAccountExists(ctx, to))
		}
		return nil
	case code == "":
		return nil // throttled
	}
	s.sent(ctx, "verification_code", s.emails.SendVerificationCode(ctx, to, code, ttl))
	if created {
		s.audit(ctx, userEvent("auth.user.registered", userID, client))
		s.accountCreated(ctx, userID)
	}
	return nil
}
