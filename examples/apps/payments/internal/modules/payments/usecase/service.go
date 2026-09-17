// Package usecase holds the payments module's operations, one file each:
// each applies the domain rules and stores the result through the Store and
// TxManager ports. The webhook route verifies the delivery's signature
// before a use case runs (delivery/routes.go).
package usecase

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"log/slog"
	"time"
)

// PermRead is the permission reading a payment requires. Permission names
// are public API.
const PermRead = "payments.payment.read"

// Service runs the payments use cases. It is safe for concurrent use.
type Service struct {
	store  Store
	tx     TxManager
	logger *slog.Logger
	now    func() time.Time
	newID  func() string
}

// NewService returns a Service reading payments from store and writing them
// through tx, which also carries the jobs a write enqueues. store and tx
// may be nil while the OpenAPI document is exported, when no use case runs.
func NewService(store Store, tx TxManager, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Service{store: store, tx: tx, logger: logger, now: time.Now, newID: newID}
}

var idEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// newID returns "pay_" and 128 random bits.
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // never fails (crypto/rand)
	return "pay_" + idEncoding.EncodeToString(b)
}

// clock returns the time at PostgreSQL's microsecond precision, so a stored
// time equals the one returned.
func (s *Service) clock() time.Time { return s.now().UTC().Truncate(time.Microsecond) }

// storeError hides driver errors, which aren't API.
func storeError(op string, err error) error {
	return fmt.Errorf("payments: %s: %v", op, err) //nolint:errorlint // driver errors aren't API
}
