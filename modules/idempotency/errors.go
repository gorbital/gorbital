package idempotency

import "errors"

// Errors returned by [Store.Claim] and [Lock] methods. Check them with
// [errors.Is].
var (
	// ErrInProgress reports a key whose first request hasn't finished. The
	// client retries later.
	ErrInProgress = errors.New("idempotency: a request with this key is in progress")

	// ErrKeyReused reports a key sent again with a different method, path,
	// query or body.
	ErrKeyReused = errors.New("idempotency: the key was used for a different request")

	// ErrLockLost reports a [Lock] that no longer holds its key: it was held
	// longer than the lock TTL and another request took the key over, or
	// the key expired and was deleted.
	ErrLockLost = errors.New("idempotency: the lock on the key was lost")
)
