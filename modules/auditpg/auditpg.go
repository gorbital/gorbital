// Package auditpg stores audit events in PostgreSQL and queries them for
// operator APIs (ADR-0036). [Store] implements [audit.Recorder]:
//
//	store, err := auditpg.NewStore(pool)
//	settingsStore, err := settings.NewStore(ctx, pool, reg, store)
//
// Events are append-only. Before storing, the store fills actor, request and
// trace fields from the context, validates the event, redacts metadata under
// sensitive keys such as "password" or "token", and bounds text lengths and
// metadata size, so one bad event never blocks the rest.
//
// Use [Store.RecordTx] to commit an event with the change it describes.
//
// Stability: pre-1.0 (ADR-0015).
package auditpg

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/audit"
	"gorbital.dev/modules/postgres"
)

// DefaultMaxMetadataBytes bounds an event's metadata after redaction.
const DefaultMaxMetadataBytes = 16 << 10

// Text column limits, in characters. Longer values are truncated.
const (
	maxActionLen    = 200
	maxKindLen      = 32
	maxIDLen        = 200
	maxLabelLen     = 200
	maxTypeLen      = 100
	maxRequestLen   = 128
	maxTraceLen     = 64
	maxUserAgentLen = 512
)

// Store records audit events in the audit_events table and lists them. It is
// safe for concurrent use.
type Store struct {
	pool             *pgxpool.Pool
	redacted         []string
	maxMetadataBytes int
	now              func() time.Time
}

var _ audit.Recorder = (*Store)(nil)

// An Option configures [NewStore].
type Option interface{ apply(*Store) }

type optionFunc func(*Store)

func (f optionFunc) apply(s *Store) { f(s) }

// WithRedactedKeys adds metadata keys whose values are replaced with
// "[REDACTED]", on top of the defaults (password, secret, token, cookie,
// authorization, api_key, private_key, otp, credential, recovery_code,
// verification_code). A key matches when it equals a name or contains it as
// a whole snake_case segment: "token" matches "refresh_token" and
// "accessToken" but not "tokenizer".
func WithRedactedKeys(names ...string) Option {
	return optionFunc(func(s *Store) {
		for _, name := range names {
			s.redacted = append(s.redacted, snakeCase(name))
		}
	})
}

// WithMaxMetadataBytes sets the largest metadata stored, as JSON after
// redaction. Larger metadata is replaced with {"metadata_dropped":
// "too_large"}. Default: [DefaultMaxMetadataBytes].
func WithMaxMetadataBytes(n int) Option {
	return optionFunc(func(s *Store) { s.maxMetadataBytes = n })
}

// NewStore returns a store on pool. The audit_events table comes from
// [Migrations]; apply them first.
func NewStore(pool *pgxpool.Pool, opts ...Option) (*Store, error) {
	s := &Store{
		pool:             pool,
		redacted:         append([]string(nil), defaultRedactedKeys...),
		maxMetadataBytes: DefaultMaxMetadataBytes,
		now:              time.Now,
	}
	for _, opt := range opts {
		opt.apply(s)
	}
	var errs []error
	if pool == nil {
		errs = append(errs, errors.New("pool is required"))
	}
	if s.maxMetadataBytes < 64 {
		errs = append(errs, fmt.Errorf("max metadata bytes %d must be at least 64", s.maxMetadataBytes))
	}
	for _, name := range s.redacted {
		if name == "" {
			errs = append(errs, errors.New("redacted key names must not be empty"))
			break
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("auditpg: invalid store: %w", err)
	}
	return s, nil
}

// Record stores e. Empty actor, organisation, request and trace fields are
// filled from ctx, and a zero OccurredAt becomes now. It returns an error for
// an invalid event or when the event can't be stored.
func (s *Store) Record(ctx context.Context, e audit.Event) error {
	return s.RecordTx(ctx, s.pool, e)
}

// RecordTx stores e through db, usually a pgx.Tx, so the event commits or
// rolls back with the change it describes.
func (s *Store) RecordTx(ctx context.Context, db postgres.DBTX, e audit.Event) error {
	row, err := s.prepare(ctx, e)
	if err != nil {
		return err
	}
	if err := insertEvent(ctx, db, row); err != nil {
		return fmt.Errorf("auditpg: record %s: %v", row.action, err) //nolint:errorlint // driver errors aren't API (ADR-0018)
	}
	return nil
}

// prepare turns e into a row that fits the table's constraints.
func (s *Store) prepare(ctx context.Context, e audit.Event) (eventRow, error) {
	e = audit.FromContext(ctx, e)
	if err := e.Validate(); err != nil {
		return eventRow{}, err
	}
	if len(e.Action) > maxActionLen {
		return eventRow{}, fmt.Errorf("audit: invalid event: action is longer than %d characters", maxActionLen)
	}
	occurred := e.OccurredAt
	if occurred.IsZero() {
		occurred = s.now()
	}
	return eventRow{
		occurredAt:   occurred.UTC(),
		actorKind:    cleanText(string(e.ActorKind), maxKindLen),
		actorID:      cleanText(e.ActorID, maxIDLen),
		actorLabel:   cleanText(e.ActorLabel, maxLabelLen),
		action:       e.Action,
		resourceType: cleanText(e.ResourceType, maxTypeLen),
		resourceID:   cleanText(e.ResourceID, maxIDLen),
		outcome:      string(e.Outcome),
		orgID:        cleanText(e.OrgID, maxIDLen),
		requestID:    cleanText(e.RequestID, maxRequestLen),
		traceID:      cleanText(e.TraceID, maxTraceLen),
		ip:           cleanIP(e.IP),
		userAgent:    cleanText(e.UserAgent, maxUserAgentLen),
		metadata:     s.encodeMetadata(e.Metadata),
	}, nil
}

// cleanText makes s storable in a text column: valid UTF-8, no NUL bytes, at
// most limit characters.
func cleanText(s string, limit int) string {
	s = strings.ReplaceAll(strings.ToValidUTF8(s, "�"), "\x00", "")
	if len(s) <= limit {
		return s
	}
	n := 0
	for i := range s {
		if n == limit {
			return s[:i]
		}
		n++
	}
	return s
}

// cleanIP returns ip in canonical form, or "" when it isn't an IP address.
func cleanIP(ip string) string {
	addr, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return ""
	}
	return addr.WithZone("").Unmap().String()
}
