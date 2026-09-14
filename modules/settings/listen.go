package settings

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

const maxReconnectDelay = 30 * time.Second

// Run keeps the registry current until ctx is done, then returns nil. It
// listens on a dedicated connection for changes committed by any instance,
// reloads everything after connecting (changes made while disconnected send
// no notification), and reloads everything every resync interval as a
// fallback. Connection failures are logged and retried with backoff.
func (s *Store) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	wg.Go(func() { s.resyncLoop(ctx) })
	defer wg.Wait()

	delay := time.Second
	for {
		connected, err := s.listen(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if connected {
			delay = time.Second
		}
		s.logger.WarnContext(ctx, "settings listener disconnected; reconnecting", "err", err, "retry_in", delay.String())
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
		delay = min(delay*2, maxReconnectDelay)
	}
}

// listen returns when the connection fails or ctx is done, reporting whether
// it got as far as listening.
func (s *Store) listen(ctx context.Context) (bool, error) {
	// A dedicated connection: waiting for notifications holds it
	// indefinitely, and cancelling the wait closes it.
	conn, err := pgx.ConnectConfig(ctx, s.pool.Config().ConnConfig.Copy())
	if err != nil {
		return false, err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = conn.Close(closeCtx)
	}()

	if _, err := conn.Exec(ctx, "LISTEN "+notifyChannel); err != nil {
		return false, err
	}
	if err := s.Reload(ctx); err != nil {
		return true, err
	}
	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return true, err
		}
		if err := s.reloadKey(ctx, n.Payload); err != nil {
			s.logger.WarnContext(ctx, "reload changed setting", "setting", n.Payload, "err", err)
		}
	}
}

func (s *Store) resyncLoop(ctx context.Context) {
	ticker := time.NewTicker(s.resync)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Reload(ctx); err != nil && ctx.Err() == nil {
				s.logger.WarnContext(ctx, "periodic settings reload failed", "err", err)
			}
		}
	}
}
