package httpx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"
)

// Server timeouts used by [NewServer] unless overridden.
const (
	DefaultReadHeaderTimeout = 5 * time.Second
	DefaultReadTimeout       = 30 * time.Second
	DefaultWriteTimeout      = 60 * time.Second
	DefaultIdleTimeout       = 120 * time.Second
	DefaultShutdownTimeout   = 20 * time.Second
	DefaultMaxHeaderBytes    = 1 << 20
)

// Server is an HTTP server that implements app.Runner.
type Server struct {
	srv             *http.Server
	shutdownTimeout time.Duration

	mu       sync.Mutex
	listener net.Listener
	ready    chan struct{}
}

// A ServerOption configures a [Server].
type ServerOption interface{ apply(*Server) }

type serverOptionFunc func(*Server)

func (f serverOptionFunc) apply(s *Server) { f(s) }

// WithTimeouts overrides read-header, read, write and idle timeouts. Zero
// values keep the defaults.
func WithTimeouts(readHeader, read, write, idle time.Duration) ServerOption {
	return serverOptionFunc(func(s *Server) {
		if readHeader > 0 {
			s.srv.ReadHeaderTimeout = readHeader
		}
		if read > 0 {
			s.srv.ReadTimeout = read
		}
		if write > 0 {
			s.srv.WriteTimeout = write
		}
		if idle > 0 {
			s.srv.IdleTimeout = idle
		}
	})
}

// WithShutdownTimeout sets how long graceful shutdown may take. Default:
// [DefaultShutdownTimeout].
func WithShutdownTimeout(d time.Duration) ServerOption {
	return serverOptionFunc(func(s *Server) { s.shutdownTimeout = d })
}

// WithErrorLogger routes the server's internal errors (for example TLS
// handshake failures) to logger.
func WithErrorLogger(logger *slog.Logger) ServerOption {
	return serverOptionFunc(func(s *Server) { s.srv.ErrorLog = slog.NewLogLogger(logger.Handler(), slog.LevelWarn) })
}

// NewServer returns a server for handler listening on addr.
func NewServer(addr string, handler http.Handler, opts ...ServerOption) *Server {
	s := &Server{
		srv: &http.Server{
			Addr:              addr,
			Handler:           handler,
			ReadHeaderTimeout: DefaultReadHeaderTimeout,
			ReadTimeout:       DefaultReadTimeout,
			WriteTimeout:      DefaultWriteTimeout,
			IdleTimeout:       DefaultIdleTimeout,
			MaxHeaderBytes:    DefaultMaxHeaderBytes,
		},
		shutdownTimeout: DefaultShutdownTimeout,
		ready:           make(chan struct{}),
	}
	for _, o := range opts {
		o.apply(s)
	}
	return s
}

// Run listens and serves until ctx is done, then shuts down gracefully:
// it stops accepting connections and waits for in-flight requests up to the
// shutdown timeout. Run returns nil after a graceful shutdown.
func (s *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.srv.Addr)
	if err != nil {
		return fmt.Errorf("httpx: listen on %s: %w", s.srv.Addr, err)
	}
	s.mu.Lock()
	s.listener = ln
	s.mu.Unlock()
	close(s.ready)

	s.srv.BaseContext = func(net.Listener) context.Context { return context.WithoutCancel(ctx) }

	errc := make(chan error, 1)
	go func() { errc <- s.srv.Serve(ln) }()

	select {
	case err := <-errc:
		return fmt.Errorf("httpx: serve: %w", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.shutdownTimeout)
	defer cancel()
	if err := s.srv.Shutdown(shutdownCtx); err != nil {
		_ = s.srv.Close()
		return fmt.Errorf("httpx: graceful shutdown: %w", err)
	}
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("httpx: serve: %w", err)
	}
	return nil
}

// Addr returns the listening address once [Server.Run] has started
// listening, blocking until then or until ctx is done. It is useful with
// port 0 in tests.
func (s *Server) Addr(ctx context.Context) (string, error) {
	select {
	case <-s.ready:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listener.Addr().String(), nil
}
