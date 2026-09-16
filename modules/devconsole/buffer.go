package devconsole

import "sync"

// buffer keeps the most recent items, at most its capacity, and hands new
// items to subscribers without ever blocking the writer.
type buffer[T any] struct {
	mu    sync.Mutex
	items []T // ring; next is the slot the next item goes to
	next  int
	full  bool
	subs  map[*subscription[T]]struct{}
}

// subscription receives items added after it subscribed. Items that don't
// fit in its channel are dropped and counted.
type subscription[T any] struct {
	ch      chan T
	dropped int // guarded by the buffer's mutex
}

func newBuffer[T any](capacity int) *buffer[T] {
	return &buffer[T]{items: make([]T, capacity), subs: map[*subscription[T]]struct{}{}}
}

// add stores v, replacing the oldest item when full, and offers it to every
// subscriber.
func (b *buffer[T]) add(v T) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.items[b.next] = v
	b.next++
	if b.next == len(b.items) {
		b.next, b.full = 0, true
	}
	for s := range b.subs {
		select {
		case s.ch <- v:
		default:
			s.dropped++
		}
	}
}

// list returns the stored items, oldest first.
func (b *buffer[T]) list() []T {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.full {
		return append([]T(nil), b.items[:b.next]...)
	}
	out := make([]T, 0, len(b.items))
	out = append(out, b.items[b.next:]...)
	return append(out, b.items[:b.next]...)
}

// capacity returns how many items the buffer keeps.
func (b *buffer[T]) capacity() int { return len(b.items) }

// subscribe returns a subscription whose channel holds up to size items.
func (b *buffer[T]) subscribe(size int) *subscription[T] {
	s := &subscription[T]{ch: make(chan T, size)}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs[s] = struct{}{}
	return s
}

// unsubscribe stops sending items to s.
func (b *buffer[T]) unsubscribe(s *subscription[T]) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.subs, s)
}

// takeDropped returns how many items s missed since the last call.
func (b *buffer[T]) takeDropped(s *subscription[T]) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := s.dropped
	s.dropped = 0
	return n
}
