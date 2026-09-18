package usecase_test

import (
	"context"
	"regexp"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/actor"
	"gorbital.dev/audit"
	authlib "gorbital.dev/modules/auth"
	"gorbital.dev/modules/postgres/pgtest"

	authrepository "example.com/invoicing/internal/modules/auth/repository"
	"example.com/invoicing/internal/modules/auth/repository/migrations"
	authusecase "example.com/invoicing/internal/modules/auth/usecase"
)

// These tests run the use cases on the real repository and Docker
// PostgreSQL, with fake emails, audit recorder and clock.

const password = "correct horse battery"

var client = authlib.ClientInfo{IP: "203.0.113.9", UserAgent: "test-agent/1"}

// requestCtx is a request's context as the middleware prepares it.
func requestCtx() context.Context {
	return authlib.WithClientInfo(context.Background(), client)
}

type sentEmail struct {
	kind, to, code string
	ttl            time.Duration
	remaining      int
}

type fakeEmails struct {
	mu   sync.Mutex
	sent []sentEmail
}

func (f *fakeEmails) add(e sentEmail) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, e)
	return nil
}

func (f *fakeEmails) SendVerificationCode(_ context.Context, to, code string, ttl time.Duration) error {
	return f.add(sentEmail{kind: "verify", to: to, code: code, ttl: ttl})
}

func (f *fakeEmails) SendPasswordResetCode(_ context.Context, to, code string, ttl time.Duration) error {
	return f.add(sentEmail{kind: "reset", to: to, code: code, ttl: ttl})
}

func (f *fakeEmails) SendAccountExists(_ context.Context, to string) error {
	return f.add(sentEmail{kind: "exists", to: to})
}

func (f *fakeEmails) SendPasswordChanged(_ context.Context, to string) error {
	return f.add(sentEmail{kind: "changed", to: to})
}

func (f *fakeEmails) SendTwoFactorEnabled(_ context.Context, to string) error {
	return f.add(sentEmail{kind: "mfa_enabled", to: to})
}

func (f *fakeEmails) SendTwoFactorDisabled(_ context.Context, to string) error {
	return f.add(sentEmail{kind: "mfa_disabled", to: to})
}

func (f *fakeEmails) SendRecoveryCodeUsed(_ context.Context, to string, remaining int) error {
	return f.add(sentEmail{kind: "recovery_used", to: to, remaining: remaining})
}

func (f *fakeEmails) SendPasskeyAdded(_ context.Context, to, name string) error {
	return f.add(sentEmail{kind: "passkey_added", to: to, code: name})
}

func (f *fakeEmails) SendSignInMethodAdded(_ context.Context, to, method string) error {
	return f.add(sentEmail{kind: "sign_in_method_added", to: to, code: method})
}

func (f *fakeEmails) SendSignInMethodRemoved(_ context.Context, to, method string) error {
	return f.add(sentEmail{kind: "sign_in_method_removed", to: to, code: method})
}

func (f *fakeEmails) SendPasskeyRemoved(_ context.Context, to, name string) error {
	return f.add(sentEmail{kind: "passkey_removed", to: to, code: name})
}

func (f *fakeEmails) last(t *testing.T, kind string) sentEmail {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.sent) - 1; i >= 0; i-- {
		if f.sent[i].kind == kind {
			return f.sent[i]
		}
	}
	t.Fatalf("no %s email was sent (sent: %+v)", kind, f.sent)
	return sentEmail{}
}

func (f *fakeEmails) count(kind string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, e := range f.sent {
		if e.kind == kind {
			n++
		}
	}
	return n
}

type recorder struct {
	mu     sync.Mutex
	events []audit.Event
}

func (r *recorder) Record(ctx context.Context, e audit.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, audit.FromContext(ctx, e))
	return nil
}

func (r *recorder) actions() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, e := range r.events {
		out = append(out, e.Action)
	}
	return out
}

func (r *recorder) find(action string) (audit.Event, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.events) - 1; i >= 0; i-- {
		if r.events[i].Action == action {
			return r.events[i], true
		}
	}
	return audit.Event{}, false
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type fixture struct {
	svc    *authusecase.Service
	emails *fakeEmails
	audit  *recorder
	clock  *clock
	pool   *pgxpool.Pool
}

func catalog() *authlib.Catalog {
	c := authlib.NewCatalog()
	c.Permission("ops.settings.read", "Read runtime settings")
	c.Permission("ops.settings.write", "Change runtime settings")
	c.Role("platform_admin", "Operates the platform", "ops.settings.read", "ops.settings.write")
	c.Role("viewer", "Reads operational data", "ops.settings.read")
	c.Permission("notes.note.read", "See your notes")
	c.Permission("notes.note.write", "Change your notes")
	c.Role(authusecase.RoleUser, "Every user", "notes.note.read", "notes.note.write")
	return c
}

func newFixture(t *testing.T, configure ...func(*authusecase.Config)) *fixture {
	t.Helper()
	f := &fixture{
		emails: &fakeEmails{},
		audit:  &recorder{},
		clock:  &clock{t: time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)},
		pool:   pgtest.New(t, pgtest.WithMigrations(migrations.FS)),
	}
	cfg := authusecase.Config{
		Store:    authrepository.NewStore(f.pool),
		Catalog:  catalog(),
		Recorder: f.audit,
		Emails:   f.emails,
		Now:      f.clock.now,
		// Timing is tested on its own (TestAnonymousFlowsTakeTheSameTime).
		MinResponseTime: -1,
	}
	for _, c := range configure {
		c(&cfg)
	}
	svc, err := authusecase.NewService(cfg)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	f.svc = svc
	return f
}

// signUp registers and verifies an account.
func (f *fixture) signUp(t *testing.T, email string) {
	t.Helper()
	if err := f.svc.Register(requestCtx(), email, password); err != nil {
		t.Fatalf("Register(%s) error = %v", email, err)
	}
	if err := f.svc.VerifyEmail(requestCtx(), email, f.emails.last(t, "verify").code); err != nil {
		t.Fatalf("VerifyEmail(%s) error = %v", email, err)
	}
}

// login signs in and returns the context of a request made with the new
// session.
func (f *fixture) login(t *testing.T, email string) (context.Context, authusecase.LoginResult) {
	t.Helper()
	res, err := f.svc.Login(requestCtx(), email, password)
	if err != nil {
		t.Fatalf("Login(%s) error = %v", email, err)
	}
	p, err := f.svc.Authenticate(context.Background(), res.Token)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	return authlib.WithPrincipal(requestCtx(), p), res
}

var sixDigits = regexp.MustCompile(`^\d{6}$`)

func operator() context.Context {
	return actor.With(context.Background(), actor.System("cli"))
}

func hasAll(got []string, want ...string) bool {
	for _, w := range want {
		if !slices.Contains(got, w) {
			return false
		}
	}
	return true
}
