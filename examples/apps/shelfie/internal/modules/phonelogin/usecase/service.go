// Package usecase holds phone-code sign-in's operations, one file each.
// They verify that a reader controls a phone number; delivery then signs
// the reader in through authhttp.
package usecase

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"math/big"
	"time"

	"gorbital.dev/actor"

	"example.com/shelfie/internal/modules/phonelogin/domain"
)

// PermWrite lets a reader set their phone number, through the user role.
const PermWrite = "phonelogin.phone.write"

// Sender sends a code in a text message. Shelfie's is a log line in
// development (LogSender); production uses an SMS provider's API.
type Sender interface {
	SendCode(ctx context.Context, phone, code string) error
}

// Store reads and writes numbers and codes; repository.Store implements it.
type Store interface {
	// InTx runs fn with a store bound to one transaction.
	InTx(ctx context.Context, fn func(tx Store) error) error
	// UpsertPhone sets a reader's number, unconfirmed.
	UpsertPhone(ctx context.Context, userID, phone string, now time.Time) error
	// ConfirmPhone confirms a reader's number, or returns ErrPhoneTaken.
	ConfirmPhone(ctx context.Context, userID, phone string, now time.Time) error
	// SelectConfirmedUser returns the account that confirmed phone, or false.
	SelectConfirmedUser(ctx context.Context, phone string) (string, bool, error)
	// SelectCodesSince returns how many codes of a purpose were sent to
	// phone since since, and when the newest was (zero without one).
	SelectCodesSince(ctx context.Context, phone, purpose string, since time.Time) (int, time.Time, error)
	InsertCode(ctx context.Context, c domain.Code) error
	// SelectActiveCode locks the newest unused, unexpired code of a purpose
	// for phone, or returns false.
	SelectActiveCode(ctx context.Context, phone, purpose string, now time.Time) (domain.Code, bool, error)
	// FailCode counts a wrong guess, using the code up at its last attempt.
	FailCode(ctx context.Context, id string, now time.Time) error
	UseCode(ctx context.Context, id string, now time.Time) error
}

// Service runs phone-code sign-in. It is safe for concurrent use.
type Service struct {
	store  Store
	sender Sender
	now    func() time.Time
	// minResponse is how long SendSignInCode takes at least, so its timing
	// doesn't tell whether a number belongs to an account.
	minResponse time.Duration
}

// NewService returns a Service on store sending codes with sender.
func NewService(store Store, sender Sender) *Service {
	return &Service{store: store, sender: sender, now: time.Now, minResponse: 300 * time.Millisecond}
}

func (s *Service) clock() time.Time { return s.now().UTC().Truncate(time.Microsecond) }

// readerID returns the signed-in reader.
func readerID(ctx context.Context) (string, error) {
	a, ok := actor.From(ctx)
	if !ok || a.Kind != actor.KindUser || a.ID == "" {
		return "", domain.ErrUnauthenticated
	}
	return a.ID, nil
}

var idEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "phc_" + idEncoding.EncodeToString(b)
}

// newCode returns 6 random digits.
func newCode() string {
	n, _ := rand.Int(rand.Reader, big.NewInt(1_000_000))
	return fmt.Sprintf("%06d", n.Int64())
}

// issueCode stores a new code for userID's phone and sends it, unless one
// was sent less than a minute ago or five in the last hour; it reports
// whether it sent one.
func (s *Service) issueCode(ctx context.Context, userID, phone, purpose string) (bool, error) {
	now := s.clock()
	sent, last, err := s.store.SelectCodesSince(ctx, phone, purpose, now.Add(-time.Hour))
	if err != nil {
		return false, err
	}
	if sent >= domain.CodesPerHour || now.Sub(last) < domain.ResendInterval {
		return false, nil // guesses stay bounded per number, whoever asks
	}
	code := newCode()
	c := domain.Code{ID: newID(), UserID: userID, Phone: phone, Purpose: purpose, Hash: domain.HashCode(code), ExpiresAt: now.Add(domain.CodeTTL), CreatedAt: now}
	if err := s.store.InsertCode(ctx, c); err != nil {
		return false, err
	}
	if err := s.sender.SendCode(ctx, phone, code); err != nil {
		return false, fmt.Errorf("%w: %v", domain.ErrSMSUnavailable, err) //nolint:errorlint // the provider's error isn't API
	}
	return true, nil
}

// checkCode uses up phone's code of purpose when code matches, and counts a
// wrong guess otherwise. It returns the code's account, or ErrInvalidCode.
func (s *Service) checkCode(ctx context.Context, phone, purpose, code string) (string, error) {
	var (
		userID string
		valid  bool
	)
	err := s.store.InTx(ctx, func(tx Store) error {
		now := s.clock()
		c, found, err := tx.SelectActiveCode(ctx, phone, purpose, now)
		if err != nil || !found {
			return err
		}
		if !c.Matches(code) {
			return tx.FailCode(ctx, c.ID, now) // commit the attempt
		}
		userID, valid = c.UserID, true
		return tx.UseCode(ctx, c.ID, now)
	})
	switch {
	case err != nil:
		return "", err
	case !valid:
		return "", domain.ErrInvalidCode
	}
	return userID, nil
}
