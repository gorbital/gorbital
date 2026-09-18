// Package repository stores the notifications module's endpoints in
// PostgreSQL with hand-written SQL, one file per operation. The table comes
// from db/migrations.
//
// One rule runs through the whole package: the url column is selected by
// exactly one query, the delivery worker's, and by nothing else. A list that
// selected it "because it was there" would put a live credential into every
// response struct, every debug print and every profile heap dump on the path
// that renders the API.
package repository

import (
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/notifications/domain"
	"example.com/plateful/internal/modules/notifications/usecase"
)

// Store implements usecase.Store on the pool.
type Store struct {
	db postgres.DBTX
}

var _ usecase.Store = (*Store)(nil)

// NewStore returns a store on pool. pool is nil while the OpenAPI document
// is exported, when no query runs.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{db: pool} }

// endpointColumns are the columns scanEndpoint reads, in its order. url is
// deliberately not among them: see the package comment.
const endpointColumns = `id, org_id, created_by, label, last_delivery_at, last_status, last_error, ` +
	`version, created_at, updated_at`

// scanEndpoint reads an endpoint without its URL.
func scanEndpoint(row pgx.CollectableRow) (domain.Endpoint, error) {
	var e domain.Endpoint
	var lastDeliveryAt *time.Time
	err := row.Scan(&e.ID, &e.OrgID, &e.CreatedBy, &e.Label, &lastDeliveryAt,
		&e.LastStatus, &e.LastError, &e.Version, &e.CreatedAt, &e.UpdatedAt)
	if lastDeliveryAt != nil {
		e.LastDeliveryAt = lastDeliveryAt.UTC()
	}
	e.CreatedAt, e.UpdatedAt = e.CreatedAt.UTC(), e.UpdatedAt.UTC()
	return e, err
}

// hostExpr is the host of the url column, computed by PostgreSQL so that the
// URL itself never crosses the wire on the path that renders a response. The
// list needs the host to show which channel a row is; it does not need, and
// must not have, the path that makes the URL a credential.
const hostExpr = `coalesce(substring(url from '://([^/?#]+)'), '')`

// scanEndpointWithHost reads an endpoint and the host of its URL, which
// comes last in the column list. The result IsZero: it knows where the
// endpoint points and not how to reach it, which is exactly what a list
// needs.
func scanEndpointWithHost(row pgx.CollectableRow) (domain.Endpoint, error) {
	var e domain.Endpoint
	var lastDeliveryAt *time.Time
	var host string
	err := row.Scan(&e.ID, &e.OrgID, &e.CreatedBy, &e.Label, &lastDeliveryAt,
		&e.LastStatus, &e.LastError, &e.Version, &e.CreatedAt, &e.UpdatedAt, &host)
	if lastDeliveryAt != nil {
		e.LastDeliveryAt = lastDeliveryAt.UTC()
	}
	e.CreatedAt, e.UpdatedAt = e.CreatedAt.UTC(), e.UpdatedAt.UTC()
	e.URL = domain.RestoreHost(host)
	return e, err
}

// scanEndpointWithURL reads an endpoint and its URL, which comes last in the
// column list. The stored URL goes through domain.RestoreURL rather than
// domain.ParseURL: a policy tightened since the row was written must not
// make the row unreadable, and the sender applies the policy again at dial
// time anyway.
func scanEndpointWithURL(row pgx.CollectableRow) (domain.Endpoint, error) {
	var e domain.Endpoint
	var lastDeliveryAt *time.Time
	var raw string
	err := row.Scan(&e.ID, &e.OrgID, &e.CreatedBy, &e.Label, &lastDeliveryAt,
		&e.LastStatus, &e.LastError, &e.Version, &e.CreatedAt, &e.UpdatedAt, &raw)
	if lastDeliveryAt != nil {
		e.LastDeliveryAt = lastDeliveryAt.UTC()
	}
	e.CreatedAt, e.UpdatedAt = e.CreatedAt.UTC(), e.UpdatedAt.UTC()
	e.URL = domain.RestoreURL(raw)
	return e, err
}

// nullTime returns t as a value the driver stores as NULL when it is zero:
// an endpoint nothing has been delivered to has no last delivery, which is
// not the same as one delivered to at the zero time.
func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// constraintError turns the constraint violations the use cases handle into
// domain errors.
func constraintError(err error) error {
	if constraint, ok := postgres.UniqueViolation(err); ok && constraint == "notification_endpoints_label" {
		return domain.ErrEndpointLabelTaken
	}
	return err
}
