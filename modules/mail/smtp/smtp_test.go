package smtp_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"io"
	"math/big"
	"mime"
	"mime/multipart"
	"net"
	netmail "net/mail"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"

	"apistock.dev/config"
	"apistock.dev/mail"
	"apistock.dev/modules/mail/smtp"
)

// session is what the fake server received in one connection.
type session struct {
	from     string
	rcpt     []string
	data     string
	user     string
	password string
	tls      bool
}

// fakeServer is a minimal SMTP server for tests.
type fakeServer struct {
	addr      string
	tlsConfig *tls.Config
	implicit  bool
	startTLS  bool
	auth      bool
	rcptReply string
	silent    bool

	mu       sync.Mutex
	sessions []session
}

func startServer(t *testing.T, s *fakeServer) *fakeServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if s.implicit {
		ln = tls.NewListener(ln, s.tlsConfig)
	}
	s.addr = ln.Addr().String()
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(conn)
		}
	}()
	return s
}

func (s *fakeServer) serve(conn net.Conn) {
	defer conn.Close()
	if s.silent {
		_, _ = io.Copy(io.Discard, conn)
		return
	}
	sess := session{tls: s.implicit}
	tp := textproto.NewConn(conn)
	_ = tp.PrintfLine("220 fake ESMTP")
	for {
		line, err := tp.ReadLine()
		if err != nil {
			return
		}
		verb, arg, _ := strings.Cut(line, " ")
		switch strings.ToUpper(verb) {
		case "EHLO", "HELO":
			replies := []string{"fake"}
			if s.startTLS && !sess.tls {
				replies = append(replies, "STARTTLS")
			}
			if s.auth {
				replies = append(replies, "AUTH PLAIN")
			}
			for i, r := range replies {
				sep := "-"
				if i == len(replies)-1 {
					sep = " "
				}
				_ = tp.PrintfLine("250%s%s", sep, r)
			}
		case "STARTTLS":
			_ = tp.PrintfLine("220 ready")
			tlsConn := tls.Server(conn, s.tlsConfig)
			if err := tlsConn.Handshake(); err != nil {
				return
			}
			tp, sess.tls = textproto.NewConn(tlsConn), true
		case "AUTH":
			_, initial, _ := strings.Cut(arg, " ")
			decoded, _ := base64.StdEncoding.DecodeString(initial)
			if parts := strings.Split(string(decoded), "\x00"); len(parts) == 3 {
				sess.user, sess.password = parts[1], parts[2]
			}
			_ = tp.PrintfLine("235 2.7.0 authenticated")
		case "MAIL":
			sess.from = arg
			_ = tp.PrintfLine("250 ok")
		case "RCPT":
			if s.rcptReply != "" {
				_ = tp.PrintfLine("%s", s.rcptReply)
				continue
			}
			sess.rcpt = append(sess.rcpt, arg)
			_ = tp.PrintfLine("250 ok")
		case "DATA":
			_ = tp.PrintfLine("354 go ahead")
			data, err := tp.ReadDotBytes()
			if err != nil {
				return
			}
			sess.data = string(data)
			s.mu.Lock()
			s.sessions = append(s.sessions, sess)
			s.mu.Unlock()
			_ = tp.PrintfLine("250 queued")
		case "QUIT":
			_ = tp.PrintfLine("221 bye")
			return
		default:
			_ = tp.PrintfLine("250 ok")
		}
	}
}

func (s *fakeServer) only(t *testing.T) session {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sessions) != 1 {
		t.Fatalf("server received %d messages, want 1", len(s.sessions))
	}
	return s.sessions[0]
}

// testTLS returns a server config with a self-signed certificate for
// 127.0.0.1, and a client config trusting it.
func testTLS(t *testing.T) (server, client *tls.Config) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "127.0.0.1"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12},
		&tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
}

func message() mail.Message {
	return mail.Message{
		From:           mail.Address{Name: "Acme Café", Email: "no-reply@acme.test"},
		To:             []mail.Address{{Name: "Ada", Email: "ada@example.com"}, {Email: "bob@example.com"}},
		ReplyTo:        []mail.Address{{Email: "support@acme.test"}},
		Subject:        "Your code: 123456 ✓",
		Text:           "Your code is 123456.\nIt expires in 15 minutes.",
		HTML:           "<p>Your code is <b>123456</b>.</p>",
		IdempotencyKey: "job-42",
	}
}

func newSender(t *testing.T, addr string, opts ...smtp.Option) *smtp.Sender {
	t.Helper()
	s, err := smtp.New(addr, opts...)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return s
}

func TestSendDeliversAMultipartMessage(t *testing.T) {
	srv := startServer(t, &fakeServer{})
	s := newSender(t, srv.addr, smtp.WithTLS(smtp.TLSNone))
	if err := s.Send(context.Background(), message()); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	got := srv.only(t)
	if got.from != "FROM:<no-reply@acme.test>" || strings.Join(got.rcpt, ",") != "TO:<ada@example.com>,TO:<bob@example.com>" {
		t.Errorf("envelope = %q %q", got.from, got.rcpt)
	}

	msg, err := netmail.ReadMessage(strings.NewReader(got.data))
	if err != nil {
		t.Fatalf("message doesn't parse: %v\n%s", err, got.data)
	}
	dec := new(mime.WordDecoder)
	subject, _ := dec.DecodeHeader(msg.Header.Get("Subject"))
	from, _ := msg.Header.AddressList("From")
	to, _ := msg.Header.AddressList("To")
	if subject != "Your code: 123456 ✓" || len(from) != 1 || from[0].Name != "Acme Café" || len(to) != 2 || msg.Header.Get("Reply-To") != "<support@acme.test>" {
		t.Errorf("headers = %v (subject %q)", msg.Header, subject)
	}
	if id := msg.Header.Get("Message-ID"); !strings.HasSuffix(id, "@acme.test>") {
		t.Errorf("Message-ID = %q", id)
	}

	_, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	parts := multipart.NewReader(msg.Body, params["boundary"])
	var bodies []string
	for {
		p, err := parts.NextPart()
		if err != nil {
			break
		}
		content, _ := io.ReadAll(p)
		bodies = append(bodies, p.Header.Get("Content-Type")+"|"+strings.ReplaceAll(string(content), "\r\n", "\n"))
	}
	want := []string{
		"text/plain; charset=utf-8|Your code is 123456.\nIt expires in 15 minutes.",
		"text/html; charset=utf-8|<p>Your code is <b>123456</b>.</p>",
	}
	if strings.Join(bodies, "||") != strings.Join(want, "||") {
		t.Errorf("parts = %q, want %q", bodies, want)
	}
}

func TestSendSameMessageIDForTheSameIdempotencyKey(t *testing.T) {
	srv := startServer(t, &fakeServer{})
	s := newSender(t, srv.addr, smtp.WithTLS(smtp.TLSNone))
	for range 2 {
		if err := s.Send(context.Background(), message()); err != nil {
			t.Fatal(err)
		}
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	ids := make([]string, 0, 2)
	for _, sess := range srv.sessions {
		msg, _ := netmail.ReadMessage(strings.NewReader(sess.data))
		ids = append(ids, msg.Header.Get("Message-ID"))
	}
	if len(ids) != 2 || ids[0] != ids[1] {
		t.Errorf("Message-IDs = %q, want the same ID for a retried send", ids)
	}
}

func TestSendTextOnly(t *testing.T) {
	srv := startServer(t, &fakeServer{})
	m := message()
	m.HTML = ""
	if err := newSender(t, srv.addr, smtp.WithTLS(smtp.TLSNone)).Send(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	msg, err := netmail.ReadMessage(strings.NewReader(srv.only(t).data))
	if err != nil {
		t.Fatal(err)
	}
	if ct := msg.Header.Get("Content-Type"); ct != "text/plain; charset=utf-8" || msg.Header.Get("Content-Transfer-Encoding") != "quoted-printable" {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestSendAuthenticatesOverStartTLS(t *testing.T) {
	serverTLS, clientTLS := testTLS(t)
	srv := startServer(t, &fakeServer{tlsConfig: serverTLS, startTLS: true, auth: true})
	s := newSender(t, srv.addr,
		smtp.WithAuth("apikey", config.NewSecret("s3cret")),
		smtp.WithTLSConfig(clientTLS),
	)
	if err := s.Send(context.Background(), message()); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if got := srv.only(t); !got.tls || got.user != "apikey" || got.password != "s3cret" {
		t.Errorf("session tls, user, password = %t, %q, %q, want authenticated after STARTTLS", got.tls, got.user, got.password)
	}
}

func TestSendImplicitTLS(t *testing.T) {
	serverTLS, clientTLS := testTLS(t)
	srv := startServer(t, &fakeServer{tlsConfig: serverTLS, implicit: true, auth: true})
	s := newSender(t, srv.addr,
		smtp.WithTLS(smtp.TLSImplicit),
		smtp.WithAuth("apikey", config.NewSecret("s3cret")),
		smtp.WithTLSConfig(clientTLS),
	)
	if err := s.Send(context.Background(), message()); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if got := srv.only(t); !got.tls || got.user != "apikey" {
		t.Errorf("session = %+v, want TLS and authenticated", got)
	}
}

func TestSendRequiresStartTLSWhenAsked(t *testing.T) {
	srv := startServer(t, &fakeServer{})
	err := newSender(t, srv.addr).Send(context.Background(), message())
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") || errors.Is(err, mail.ErrRejected) {
		t.Errorf("Send() to a server without STARTTLS error = %v, want a temporary STARTTLS error", err)
	}
}

func TestSendClassifiesRefusals(t *testing.T) {
	permanent := startServer(t, &fakeServer{rcptReply: "550 5.1.1 no such user"})
	err := newSender(t, permanent.addr, smtp.WithTLS(smtp.TLSNone)).Send(context.Background(), message())
	if !errors.Is(err, mail.ErrRejected) || !strings.Contains(err.Error(), "550") {
		t.Errorf("Send() with a 550 reply error = %v, want ErrRejected", err)
	}

	temporary := startServer(t, &fakeServer{rcptReply: "451 4.3.0 try again later"})
	err = newSender(t, temporary.addr, smtp.WithTLS(smtp.TLSNone)).Send(context.Background(), message())
	if err == nil || errors.Is(err, mail.ErrRejected) {
		t.Errorf("Send() with a 451 reply error = %v, want a temporary error", err)
	}

	invalid := message()
	invalid.To = nil
	if err := newSender(t, permanent.addr, smtp.WithTLS(smtp.TLSNone)).Send(context.Background(), invalid); !errors.Is(err, mail.ErrRejected) {
		t.Errorf("Send(invalid message) error = %v, want ErrRejected", err)
	}
}

func TestSendRespectsTimeoutAndCancellation(t *testing.T) {
	srv := startServer(t, &fakeServer{silent: true})
	start := time.Now()
	err := newSender(t, srv.addr, smtp.WithTLS(smtp.TLSNone), smtp.WithTimeout(200*time.Millisecond)).Send(context.Background(), message())
	if err == nil || errors.Is(err, mail.ErrRejected) || time.Since(start) > 5*time.Second {
		t.Errorf("Send() to a silent server = %v after %v, want a timeout", err, time.Since(start))
	}

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	err = newSender(t, srv.addr, smtp.WithTLS(smtp.TLSNone)).Send(ctx, message())
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Send() cancelled error = %v, want context.Canceled", err)
	}
}

func TestNewValidates(t *testing.T) {
	tests := []struct {
		name string
		addr string
		opts []smtp.Option
	}{
		{"no port", "smtp.example.com", nil},
		{"bad mode", "smtp.example.com:587", []smtp.Option{smtp.WithTLS("ssl")}},
		{"username without password", "smtp.example.com:587", []smtp.Option{smtp.WithAuth("user", config.Secret{})}},
		{"credentials in plain text", "smtp.example.com:25", []smtp.Option{smtp.WithTLS(smtp.TLSNone), smtp.WithAuth("user", config.NewSecret("pw"))}},
		{"zero timeout", "smtp.example.com:587", []smtp.Option{smtp.WithTimeout(0)}},
	}
	for _, tt := range tests {
		if _, err := smtp.New(tt.addr, tt.opts...); err == nil {
			t.Errorf("%s: New() error = nil", tt.name)
		}
	}
	if _, err := smtp.New("localhost:1025", smtp.WithTLS(smtp.TLSNone), smtp.WithAuth("user", config.NewSecret("pw"))); err != nil {
		t.Errorf("New(local server without TLS) error = %v", err)
	}
	for in, want := range map[string]smtp.TLSMode{"STARTTLS": smtp.TLSStartTLS, " tls ": smtp.TLSImplicit, "none": smtp.TLSNone} {
		if got, err := smtp.ParseTLSMode(in); err != nil || got != want {
			t.Errorf("ParseTLSMode(%q) = %q, %v", in, got, err)
		}
	}
}
