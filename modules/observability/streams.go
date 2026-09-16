package observability

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Errors of [Streams].
var (
	// ErrTooManyStreams reports a stream refused because the instance, or
	// the subject, already has the most streams allowed.
	ErrTooManyStreams = errors.New("observability: too many streams")
	// ErrStreamExpired is the cause of a stream context that reached its
	// maximum duration.
	ErrStreamExpired = errors.New("observability: stream reached its maximum duration")
	// ErrStreamsClosed is the cause of stream contexts ended by
	// [Streams.Close], and the error of Open afterwards.
	ErrStreamsClosed = errors.New("observability: streams closed")
)

// Streams bounds long-lived responses such as Server-Sent Events on one
// instance: how many run at once, how many one subject (such as a user) may
// hold, and how long each lasts. It is safe for concurrent use.
type Streams struct {
	maxTotal, maxPerSubject int
	maxDuration             time.Duration

	mu        sync.Mutex
	total     int
	bySubject map[string]int
	closed    bool
	cancels   map[*int]context.CancelCauseFunc
}

// NewStreams returns limits of maxTotal streams, maxPerSubject per subject,
// each lasting at most maxDuration.
func NewStreams(maxTotal, maxPerSubject int, maxDuration time.Duration) (*Streams, error) {
	if maxTotal < 1 || maxPerSubject < 1 || maxDuration <= 0 {
		return nil, errors.New("observability: stream limits must be positive")
	}
	return &Streams{
		maxTotal: maxTotal, maxPerSubject: maxPerSubject, maxDuration: maxDuration,
		bySubject: map[string]int{}, cancels: map[*int]context.CancelCauseFunc{},
	}, nil
}

// MaxDuration returns how long a stream lasts at most.
func (s *Streams) MaxDuration() time.Duration { return s.maxDuration }

// Open starts a stream for subject. The returned context ends with ctx,
// after the maximum duration (cause [ErrStreamExpired]) or when the
// streams are closed (cause [ErrStreamsClosed]); call done when the stream
// ends. Open returns [ErrTooManyStreams] over a limit.
func (s *Streams) Open(ctx context.Context, subject string) (streamCtx context.Context, done func(), err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.closed:
		return nil, nil, ErrStreamsClosed
	case s.total >= s.maxTotal || s.bySubject[subject] >= s.maxPerSubject:
		return nil, nil, ErrTooManyStreams
	}
	s.total++
	s.bySubject[subject]++

	streamCtx, cancelTimeout := context.WithTimeoutCause(ctx, s.maxDuration, ErrStreamExpired)
	streamCtx, cancel := context.WithCancelCause(streamCtx)
	key := new(int)
	s.cancels[key] = cancel
	var once sync.Once
	return streamCtx, func() {
		once.Do(func() {
			cancel(context.Canceled)
			cancelTimeout()
			s.mu.Lock()
			defer s.mu.Unlock()
			delete(s.cancels, key)
			s.total--
			if s.bySubject[subject]--; s.bySubject[subject] <= 0 {
				delete(s.bySubject, subject)
			}
		})
	}, nil
}

// Close ends every open stream and refuses new ones, for a shutting-down
// instance: load balancers and HTTP servers wait for long-lived responses.
func (s *Streams) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	for _, cancel := range s.cancels {
		cancel(ErrStreamsClosed)
	}
}

// Count returns how many streams are open.
func (s *Streams) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.total
}
