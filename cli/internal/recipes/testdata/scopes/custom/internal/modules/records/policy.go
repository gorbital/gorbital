package records

import (
	"context"

	"gorbital.dev/gorbital"

	"example.com/app/internal/modules/records/domain"
	"example.com/app/internal/modules/records/repository"
)

// Policy decides who may see and change a record. gorbital does not know,
// so nothing here is implemented: fill these in and delete the errors.
//
// This file is yours from the moment it landed. orb generated it once and
// will not write it again, and orb upgrade never touches it.
//
// Each method returns nil to allow the caller through,
// domain.ErrRecordNotFound to refuse — a record somebody may not see must
// not exist for them, so they can't tell one apart from a missing one —
// or domain.ErrUnauthenticated when the caller isn't signed in at all.
// While a method returns gorbital.ErrNotImplemented the module answers 501
// not_implemented, policy_test.go fails, and orb doctor names this module.
type Policy struct {
	// The app's dependencies: whatever answering the questions below
	// needs. Module() in module.go builds this, so add its arguments
	// there.
}

// CanRead reports whether the actor in ctx may read this record. It runs
// for every single record the module returns by ID.
func (p Policy) CanRead(ctx context.Context, record domain.Record) error {
	return gorbital.ErrNotImplemented // decide: which actors may read one record?
}

// CanWrite reports whether the actor in ctx may create, update or delete
// it. On an update it is asked about the record as it is now, inside the
// transaction that locked it.
func (p Policy) CanWrite(ctx context.Context, record domain.Record) error {
	return gorbital.ErrNotImplemented // decide: who may change one, and in which states?
}

// Filter narrows a list query to the rows the actor in ctx may see. It is
// the only thing standing between a list request and every row in the
// table: what this function doesn't exclude, GET /v1/records returns.
//
// Narrow it with q.And, whose ? placeholders take the arguments after it:
//
//	q.And("created_by = ?", actorID)
func (p Policy) Filter(ctx context.Context, q *repository.Query) error {
	return gorbital.ErrNotImplemented // decide: which rows appear in a list?
}
