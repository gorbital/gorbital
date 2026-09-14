// Package app runs an application's long-running work and shuts it down in a
// defined order.
//
// Constructors do blocking setup (open a pool, load keys) and register what
// they open on a [Cleanup] stack. Long-running work, such as an HTTP server or
// a job worker, implements [Runner]. [Run] starts the runners and, when a
// signal arrives, the context ends or a runner fails, performs the shutdown
// sequence documented on [Run].
//
// A typical composition root:
//
//	cleanup := &app.Cleanup{}
//	db, err := postgres.Open(ctx, dsn)
//	if err != nil {
//		return errors.Join(err, cleanup.Close(ctx))
//	}
//	cleanup.AddCloser("postgres", db)
//	return app.Run(ctx, []app.Runner{server, workers}, app.WithCleanup(cleanup))
//
// Stability: pre-1.0; the API may change in minor releases (ADR-0015).
// Design: ADR-0017.
package app
