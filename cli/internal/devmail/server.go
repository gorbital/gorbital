// Package devmail is the Dev Portal's mail catcher (ADR-0074): an SMTP
// server inside orb dev that keeps every message the app sends under
// .orb/portal/mail, parsed for the inbox screen. It speaks the SMTP the
// app's sender needs in development (no TLS, no authentication) and
// nothing more; it must never listen beyond the loopback interface.
package devmail

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

// Limits of a session.
const (
	// MaxMessageBytes is the largest message accepted.
	MaxMessageBytes = 16 << 20
	maxRecipients   = 100
	sessionTimeout  = 2 * time.Minute
)

// Server receives messages into a Store.
type Server struct {
	store    *Store
	hostname string
	ln       net.Listener
	mu       sync.Mutex
	conns    map[net.Conn]struct{}
	wg       sync.WaitGroup
}

// NewServer returns a server storing into store.
func NewServer(store *Store) *Server {
	return &Server{store: store, hostname: "orb.dev.local", conns: map[net.Conn]struct{}{}}
}

// Listen starts accepting on addr, which must be a loopback address. It
// returns once the listener is open; sessions run until Close.
func (s *Server) Listen(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("devmail: %q is not host:port", addr)
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("devmail: %q isn't a loopback address; the mail catcher only listens on this machine", addr)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.ln = ln
	s.wg.Add(1)
	go s.accept()
	return nil
}

// Addr is the listening address.
func (s *Server) Addr() string {
	if s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}

// Close stops listening and ends every session.
func (s *Server) Close() error {
	var err error
	if s.ln != nil {
		err = s.ln.Close()
	}
	s.mu.Lock()
	for c := range s.conns {
		_ = c.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
	return err
}

func (s *Server) accept() {
	defer s.wg.Done()
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.conns[conn] = struct{}{}
		s.mu.Unlock()
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.session(conn)
			s.mu.Lock()
			delete(s.conns, conn)
			s.mu.Unlock()
		}()
	}
}

// session runs one SMTP conversation.
func (s *Server) session(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(sessionTimeout))
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	reply := func(code int, text string) bool {
		_, err := fmt.Fprintf(w, "%d %s\r\n", code, text)
		if err == nil {
			err = w.Flush()
		}
		return err == nil
	}
	if !reply(220, s.hostname+" orb dev mail catcher") {
		return
	}
	var from string
	var rcpts []string
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		verb, arg, _ := strings.Cut(line, " ")
		switch strings.ToUpper(verb) {
		case "EHLO":
			_, _ = fmt.Fprintf(w, "250-%s greets %s\r\n250-8BITMIME\r\n250-SIZE %d\r\n250 SMTPUTF8\r\n", s.hostname, arg, MaxMessageBytes)
			if w.Flush() != nil {
				return
			}
		case "HELO":
			if !reply(250, s.hostname) {
				return
			}
		case "MAIL":
			from = address(arg, "FROM:")
			rcpts = nil
			if !reply(250, "OK") {
				return
			}
		case "RCPT":
			if len(rcpts) >= maxRecipients {
				if !reply(452, "too many recipients") {
					return
				}
				continue
			}
			rcpts = append(rcpts, address(arg, "TO:"))
			if !reply(250, "OK") {
				return
			}
		case "DATA":
			if len(rcpts) == 0 {
				if !reply(503, "RCPT TO first") {
					return
				}
				continue
			}
			if !reply(354, "End data with <CR><LF>.<CR><LF>") {
				return
			}
			data, err := readData(r)
			if err != nil {
				if errors.Is(err, errTooLarge) {
					_ = reply(552, "message too large")
				}
				return
			}
			if _, err := s.store.Add(context.Background(), from, rcpts, data); err != nil {
				if !reply(451, "couldn't store the message: "+err.Error()) {
					return
				}
				continue
			}
			from, rcpts = "", nil
			if !reply(250, "OK: queued") {
				return
			}
		case "RSET":
			from, rcpts = "", nil
			if !reply(250, "OK") {
				return
			}
		case "NOOP":
			if !reply(250, "OK") {
				return
			}
		case "QUIT":
			_ = reply(221, "Bye")
			return
		case "STARTTLS":
			if !reply(454, "TLS not available; the mail catcher is plain text on this machine") {
				return
			}
		case "AUTH":
			if !reply(503, "no authentication needed") {
				return
			}
		default:
			if !reply(500, "unknown command") {
				return
			}
		}
	}
}

var errTooLarge = errors.New("message too large")

// readData reads the message up to the lone dot, undoing dot-stuffing.
func readData(r *bufio.Reader) ([]byte, error) {
	var b strings.Builder
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}
		if line == ".\r\n" || line == ".\n" {
			return []byte(b.String()), nil
		}
		line = strings.TrimPrefix(line, ".")
		if b.Len()+len(line) > MaxMessageBytes {
			return nil, errTooLarge
		}
		b.WriteString(line)
	}
}

// address extracts the address from "FROM:<a@b>" or "TO:<a@b> params".
func address(arg, prefix string) string {
	arg = strings.TrimSpace(arg)
	if rest, ok := strings.CutPrefix(strings.ToUpper(arg[:min(len(arg), len(prefix))]), prefix); ok {
		_ = rest
		arg = strings.TrimSpace(arg[len(prefix):])
	}
	if i := strings.IndexByte(arg, ' '); i >= 0 {
		arg = arg[:i]
	}
	return strings.Trim(arg, "<>")
}
