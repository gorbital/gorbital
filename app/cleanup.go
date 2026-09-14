package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
)

// Cleanup is a stack of resources to release, closed in reverse order of
// registration. The zero value is ready to use. It is safe for concurrent use.
type Cleanup struct {
	mu    sync.Mutex
	items []cleanupItem
}

type cleanupItem struct {
	name  string
	close func(context.Context) error
}

// Add registers fn to run on [Cleanup.Close]. The name appears in errors.
func (c *Cleanup) Add(name string, fn func(ctx context.Context) error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = append(c.items, cleanupItem{name: name, close: fn})
}

// AddCloser registers an [io.Closer].
func (c *Cleanup) AddCloser(name string, closer io.Closer) {
	c.Add(name, func(context.Context) error { return closer.Close() })
}

// Close runs the registered functions in reverse order and removes them. Every
// function runs even if an earlier one fails; the errors are joined. Calling
// Close again runs only functions added since the previous call.
func (c *Cleanup) Close(ctx context.Context) error {
	c.mu.Lock()
	items := c.items
	c.items = nil
	c.mu.Unlock()

	var errs []error
	for i := len(items) - 1; i >= 0; i-- {
		if err := items[i].close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("app: close %s: %w", items[i].name, err))
		}
	}
	return errors.Join(errs...)
}
