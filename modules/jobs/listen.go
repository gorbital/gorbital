package jobs

import (
	"context"
	"maps"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

const maxReconnectDelay = 30 * time.Second

// Run keeps job definitions and schedules current until ctx is done, then
// returns nil. It listens on a dedicated connection for changes committed by
// any instance, reloads everything after connecting, and reloads everything
// every resync interval as a fallback. Connection failures are logged and
// retried with backoff.
func (m *Manager) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	wg.Go(func() { m.resyncLoop(ctx) })
	defer wg.Wait()

	delay := time.Second
	for {
		connected, err := m.listen(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if connected {
			delay = time.Second
		}
		m.logger.WarnContext(ctx, "jobs listener disconnected; reconnecting", "err", err, "retry_in", delay.String())
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

func (m *Manager) listen(ctx context.Context) (bool, error) {
	conn, err := pgx.ConnectConfig(ctx, m.pool.Config().ConnConfig.Copy())
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
	if err := m.Reload(ctx); err != nil {
		return true, err
	}
	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return true, err
		}
		if err := m.reloadDefinition(ctx, n.Payload); err != nil {
			m.logger.WarnContext(ctx, "reload changed job definition", "definition", n.Payload, "err", err)
		}
	}
}

func (m *Manager) resyncLoop(ctx context.Context) {
	ticker := time.NewTicker(m.resync)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := m.Reload(ctx); err != nil && ctx.Err() == nil {
				m.logger.WarnContext(ctx, "periodic job definitions reload failed", "err", err)
			}
		}
	}
}

// replaceOverrides installs a full reload, keeping any newer version already
// applied.
func (defs *Definitions) replaceOverrides(loaded map[string]override) {
	defs.overridesMu.Lock()
	defer defs.overridesMu.Unlock()
	if cur := defs.overrides.Load(); cur != nil {
		for name, old := range *cur {
			if o, ok := loaded[name]; !ok || old.version > o.version {
				loaded[name] = old
			}
		}
	}
	defs.overrides.Store(&loaded)
}

// applyOverride stores one override unless a newer version is known.
func (defs *Definitions) applyOverride(name string, o override) {
	defs.overridesMu.Lock()
	defer defs.overridesMu.Unlock()
	next := make(map[string]override)
	if cur := defs.overrides.Load(); cur != nil {
		if old, ok := (*cur)[name]; ok && old.version > o.version {
			return
		}
		maps.Copy(next, *cur)
	}
	next[name] = o
	defs.overrides.Store(&next)
}
