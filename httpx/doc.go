// Package httpx provides the HTTP foundation of a gorbital app: a server
// that runs under app.Run with safe timeouts, security middleware, and the
// RFC 9457 problem+json error contract with an application-owned error
// mapping (ADR-0018).
//
// A typical middleware chain, outermost first:
//
//	handler := httpx.Chain(mux,
//		httpx.Recover(logger),
//		httpx.RequestID(),
//		httpx.AccessLog(logger),
//		httpx.SecureHeaders(httpx.SecureHeadersOptions{}),
//		cors,
//		crossOrigin,
//		httpx.BodyLimit(1<<20),
//	)
//
// Stability: stable (ADR-0015, ADR-0054).
package httpx
