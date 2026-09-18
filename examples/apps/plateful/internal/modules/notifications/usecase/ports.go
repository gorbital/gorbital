package usecase

import (
	"context"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"example.com/plateful/internal/modules/notifications/domain"
)

// Store reads and writes notification endpoints; repository.Store implements
// it with SQL.
type Store interface {
	// InsertEndpoint stores a new endpoint, or returns
	// ErrEndpointLabelTaken.
	InsertEndpoint(ctx context.Context, e domain.Endpoint) (domain.Endpoint, error)
	// SelectEndpoints returns every endpoint of orgID, oldest first, with the
	// host each points at but *not* its URL: the list is what the API returns
	// and what the fanout job walks, and neither has any use for the secret.
	SelectEndpoints(ctx context.Context, orgID string) ([]domain.Endpoint, error)
	// SelectEndpointWithURL returns one of orgID's endpoints including its
	// URL, or ErrEndpointNotFound. The delivery worker is its only caller.
	SelectEndpointWithURL(ctx context.Context, orgID, id string) (domain.Endpoint, error)
	// DeleteEndpoint removes one of orgID's endpoints, or returns
	// ErrEndpointNotFound.
	DeleteEndpoint(ctx context.Context, orgID, id string) error
	// UpdateDelivery records the outcome of e's last delivery attempt. A
	// missing row is not an error: the endpoint may have been removed while
	// the delivery was in flight.
	UpdateDelivery(ctx context.Context, e domain.Endpoint) error
}

// A Sender posts one message to one endpoint. The module's own sender
// implements it; the interface exists so the use cases stay testable and so
// a second channel — email, a provider's SDK — could be added without the
// worker changing.
//
// Send returns the HTTP status the endpoint answered, 0 when it never
// answered, and an error wrapping domain.ErrDeliveryFailed when the attempt
// may work next time or domain.ErrDeliveryRejected when it never will. The
// error must never contain the endpoint's URL.
type Sender interface {
	Send(ctx context.Context, target domain.URL, m domain.Message) (int, error)
}

// An Enqueuer inserts a job. Both *jobs.Client and River's own
// *river.Client[pgx.Tx] satisfy it, which is what lets the fanout worker
// take whichever one it can get: the module can't hand it a client at
// startup, because gorbital.Deps.Jobs is always nil inside Module.Jobs — the
// client does not exist yet when the definitions are declared — so in
// production the worker takes River's from its own context, and a test
// passes one in.
type Enqueuer interface {
	Insert(ctx context.Context, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error)
}
