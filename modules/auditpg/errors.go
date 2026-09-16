package auditpg

import "errors"

// Errors returned by [Store] methods. Check them with [errors.Is].
var (
	// ErrEventNotFound reports an event ID that doesn't exist, or was
	// removed by retention.
	ErrEventNotFound = errors.New("auditpg: audit event not found")

	// ErrInvalidCursor reports a cursor that wasn't returned by [Store.List].
	ErrInvalidCursor = errors.New("auditpg: invalid cursor")

	// ErrInvalidFilter reports a [Filter] with an unknown outcome, a
	// malformed action prefix or an empty time range. The wrapping error
	// says which.
	ErrInvalidFilter = errors.New("auditpg: invalid filter")

	// ErrQueryTimeout reports a list or stats query that ran longer than
	// the store's query timeout, usually because no index serves its
	// filters. Narrow the filters or the time range.
	ErrQueryTimeout = errors.New("auditpg: audit query timed out")
)
