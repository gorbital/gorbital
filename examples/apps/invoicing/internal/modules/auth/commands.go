package authhttp

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/actor"
	"gorbital.dev/gorbital"
	"gorbital.dev/modules/auditpg"
	authlib "gorbital.dev/modules/auth"
	"gorbital.dev/modules/postgres"

	"example.com/invoicing/internal/modules/auth/domain"
	"example.com/invoicing/internal/modules/auth/repository"
	"example.com/invoicing/internal/modules/auth/usecase"
)

// Commands returns sign-in's commands, which gorbital.Main serves beside
// its own, with the arguments, output and messages of a v0.1 app's
// cmd/api:
//
//	roles                        list the platform roles and their permissions
//	grant-role <email> <role>    give an account a platform role
//	revoke-role <email> <role>   take a platform role away
//	reset-mfa <email>            turn off an account's two-factor authentication
//	rotate-auth-keys             re-encrypt 2FA secrets with the first AUTH_ENCRYPTION_KEYS key
//	auth-providers               show which sign-in methods are configured
//	seed [--email <email>]       create a development administrator with two-factor authentication
//
// Changes are recorded in the audit log as the "cli" system actor. Wrong
// arguments exit with status 2 (gorbital.ErrUsage), as every command of
// gorbital.Main does; other failures, such as an unknown account, with 1.
func (a *Authenticator) Commands() []gorbital.Command {
	return []gorbital.Command{
		{Name: "roles", Usage: "roles                          list the platform roles and their permissions", Run: a.runRoles},
		{Name: "grant-role", Usage: "grant-role <email> <role>      give an account a platform role", Run: a.runChangeRole(true)},
		{Name: "revoke-role", Usage: "revoke-role <email> <role>     take a platform role away", Run: a.runChangeRole(false)},
		{Name: "reset-mfa", Usage: "reset-mfa <email>              turn off an account's two-factor authentication", Run: a.runResetMFA},
		{Name: "rotate-auth-keys", Usage: "rotate-auth-keys               re-encrypt 2FA secrets with the first AUTH_ENCRYPTION_KEYS key", Run: a.runRotateAuthKeys},
		{Name: "auth-providers", Usage: "auth-providers                 show which sign-in methods are configured", Run: a.runAuthProviders},
		{Name: "seed", Usage: "seed [--email <email>]         create a development administrator with 2FA (orb dev runs it)", Run: a.runSeed},
	}
}

// setupCatalog returns the permission catalog Setup received.
func (a *Authenticator) setupCatalog() (*authlib.Catalog, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.catalog == nil {
		return nil, errors.New("authhttp: a command ran before Setup: run it through gorbital.Main")
	}
	return a.catalog, nil
}

// appName returns the app's name Setup received.
func (a *Authenticator) appName() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.name
}

// runRoles lists the platform roles and their permissions.
func (a *Authenticator) runRoles(_ context.Context, _ gorbital.Config, _ []string, w io.Writer) error {
	catalog, err := a.setupCatalog()
	if err != nil {
		return err
	}
	for _, r := range catalog.Roles() {
		fmt.Fprintf(w, "%s\n  %s\n  permissions: %s\n", r.Name, r.Description, strings.Join(r.Permissions, ", "))
	}
	return nil
}

func (a *Authenticator) runChangeRole(grant bool) func(context.Context, gorbital.Config, []string, io.Writer) error {
	name := "revoke-role"
	if grant {
		name = "grant-role"
	}
	return func(ctx context.Context, cfg gorbital.Config, args []string, w io.Writer) error {
		if len(args) != 2 {
			return fmt.Errorf("%w: %s <email> <role>", gorbital.ErrUsage, name)
		}
		email, role := args[0], args[1]
		deps, err := a.openCommandDeps(ctx, cfg, "cli")
		if err != nil {
			return err
		}
		defer deps.pool.Close()
		svc := deps.auth

		ctx = actor.With(ctx, actor.System("cli"))
		user, err := svc.UserByEmail(ctx, email)
		if errors.Is(err, domain.ErrUserNotFound) {
			return fmt.Errorf("no account uses %s: register it first (POST /v1/auth/register) and verify the email", email)
		}
		if err != nil {
			return err
		}
		if grant {
			err = svc.GrantRole(ctx, user.ID, role)
		} else {
			err = svc.RevokeRole(ctx, user.ID, role)
		}
		if errors.Is(err, domain.ErrEmailNotVerified) {
			return fmt.Errorf("%s hasn't verified its email address; roles are granted only to verified accounts. "+
				"Ask the user to verify with the emailed code (or reset the password, which verifies it), then grant the role again", email)
		}
		if errors.Is(err, domain.ErrUnknownRole) && role == usecase.RoleUser {
			return fmt.Errorf("every account holds the %s role without a grant", role)
		}
		if errors.Is(err, domain.ErrUnknownRole) {
			var names []string
			for _, r := range svc.Catalog().Roles() {
				if r.Name != usecase.RoleUser {
					names = append(names, r.Name)
				}
			}
			return fmt.Errorf("unknown role %q; roles are: %s", role, strings.Join(names, ", "))
		}
		if err != nil {
			return err
		}
		if user, err = svc.User(ctx, user.ID); err != nil {
			return err
		}
		roles := "none"
		if len(user.Roles) > 0 {
			roles = strings.Join(user.Roles, ", ")
		}
		fmt.Fprintf(w, "✓ %s now has roles: %s\n", email, roles)
		return nil
	}
}

// runResetMFA turns off two-factor authentication for the account registered
// with email, for a user who lost their authenticator app and recovery
// codes, and ends its sessions.
func (a *Authenticator) runResetMFA(ctx context.Context, cfg gorbital.Config, args []string, w io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("%w: reset-mfa <email>", gorbital.ErrUsage)
	}
	email := args[0]
	deps, err := a.openCommandDeps(ctx, cfg, "cli")
	if err != nil {
		return err
	}
	defer deps.pool.Close()

	ctx = actor.With(ctx, actor.System("cli"))
	user, err := deps.auth.UserByEmail(ctx, email)
	if errors.Is(err, domain.ErrUserNotFound) {
		return fmt.Errorf("no account uses %s", email)
	}
	if err != nil {
		return err
	}
	switch err := deps.auth.ResetMFA(ctx, user.ID); {
	case errors.Is(err, domain.ErrMFANotEnabled):
		return fmt.Errorf("%s doesn't have two-factor authentication on", email)
	case err != nil:
		return err
	}
	fmt.Fprintf(w, "✓ Two-factor authentication is off for %s, and its sessions ended.\n", email)
	if deps.auth.Catalog().RequiresMFA(user.Roles...) {
		fmt.Fprintln(w, "  Its roles require two-factor authentication: it has to turn it on again before using them.")
	}
	return nil
}

// runRotateAuthKeys re-encrypts every authenticator app secret with the
// first key of AUTH_ENCRYPTION_KEYS. Deploy every instance with the new key
// first and the old keys after it, run this, then remove the old keys.
func (a *Authenticator) runRotateAuthKeys(ctx context.Context, cfg gorbital.Config, _ []string, w io.Writer) error {
	deps, err := a.openCommandDeps(ctx, cfg, "cli")
	if err != nil {
		return err
	}
	defer deps.pool.Close()

	n, err := deps.auth.RotateEncryptionKeys(actor.With(ctx, actor.System("cli")))
	if errors.Is(err, domain.ErrMFAUnavailable) {
		return errors.New("AUTH_ENCRYPTION_KEYS is empty: set the new key first, followed by the old keys")
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "✓ %d secrets re-encrypted with key %q. Remove the old keys from AUTH_ENCRYPTION_KEYS once every instance runs with this list.\n",
		n, keyring(cfg).CurrentKeyID())
	return nil
}

// runAuthProviders prints which sign-in methods are configured.
// gorbital.LoadConfig and CheckConfig have refused an invalid or partial
// configuration.
func (a *Authenticator) runAuthProviders(_ context.Context, cfg gorbital.Config, _ []string, w io.Writer) error {
	writeSignInMethods(w, cfg)
	return nil
}

// defaultSeedEmail is the administrator the seed command creates unless
// --email names another.
const defaultSeedEmail = "admin@example.com"

// runSeed creates a development administrator, as a v0.1 app's cmd/seed
// does (ADR-0042): an account with a verified address, the platform_admin
// role and two-factor authentication on, because the role requires it
// (ADR-0043). Its random password, authenticator app key and recovery codes
// are printed once and stored nowhere in plain text. It refuses production
// and changes nothing when the account exists, so orb dev runs it on every
// start. Everything goes through sign-in's use cases, so the password
// policy, hashing, encryption and audit events apply.
//
// Unlike v0.1's cmd/seed it creates no example records: those belong to the
// app's modules. In an app with organisations, the administrator's personal
// workspace is created the first time it lists its organisations.
func (a *Authenticator) runSeed(ctx context.Context, cfg gorbital.Config, args []string, w io.Writer) error {
	flags := flag.NewFlagSet("seed", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	email := flags.String("email", defaultSeedEmail, "email address of the administrator to create")
	if err := flags.Parse(args); err != nil || flags.NArg() > 0 {
		return fmt.Errorf("%w: seed [--email <email>]", gorbital.ErrUsage)
	}
	if cfg.Production() {
		return errors.New("seed data is for development only, and APP_ENV is production")
	}
	deps, err := a.openCommandDeps(ctx, cfg, "seed")
	if err != nil {
		return err
	}
	defer deps.pool.Close()
	if keyring(cfg) == nil {
		return errors.New("AUTH_ENCRYPTION_KEYS is required: the administrator's role needs two-factor authentication, whose secrets it encrypts (orb dev sets it in .env)")
	}
	svc := deps.auth

	ctx = actor.With(ctx, actor.System("seed"))
	_, err = svc.UserByEmail(ctx, *email)
	switch {
	case err == nil:
		fmt.Fprintf(w, "✓ Seed data is in place: %s exists and was left unchanged.\n"+
			"  Lost its password? POST /v1/auth/password/forgot, then use the emailed code.\n", *email)
		return nil
	case !errors.Is(err, domain.ErrUserNotFound):
		return err
	}

	password := rand.Text()
	admin, err := svc.CreateUser(ctx, *email, password, true)
	if err != nil {
		return fmt.Errorf("create the administrator: %w", err)
	}
	if err := svc.GrantRole(ctx, admin.ID, "platform_admin"); err != nil {
		return fmt.Errorf("grant platform_admin to the administrator: %w", err)
	}
	enrollment, recoveryCodes, err := svc.EnrollTOTP(ctx, admin.ID)
	if err != nil {
		return fmt.Errorf("turn on two-factor authentication for the administrator: %w", err)
	}
	half := len(recoveryCodes) / 2
	fmt.Fprintf(w, `✓ Seed data created
  Administrator:   %s (platform_admin)
  Password:        %s
  2FA key:         %s
  2FA QR code URI: %s
  Recovery codes:  %s
                   %s

  These are shown only this once and aren't saved anywhere. Add the 2FA key to
  an authenticator app: signing in as the administrator asks for its code, and
  /ops needs it. Lost the password? POST /v1/auth/password/forgot and the
  emailed code. Lost the authenticator app? Sign in with a recovery code.
`, admin.Email, password, enrollment.Secret, enrollment.URI,
		strings.Join(recoveryCodes[:half], "  "), strings.Join(recoveryCodes[half:], "  "))
	return nil
}

// commandDeps are what commands outside the server need: the database and
// the auth use cases, without HTTP or workers.
type commandDeps struct {
	pool *pgxpool.Pool
	auth *usecase.Service
}

// openCommandDeps connects to the database as "<app>-<name>". Close
// deps.pool when done.
func (a *Authenticator) openCommandDeps(ctx context.Context, cfg gorbital.Config, name string) (commandDeps, error) {
	catalog, err := a.setupCatalog()
	if err != nil {
		return commandDeps{}, err
	}
	if cfg.DatabaseURL.IsZero() {
		return commandDeps{}, errors.New("DATABASE_URL is required")
	}
	pool, err := postgres.Open(ctx, cfg.DatabaseURL, postgres.WithApplicationName(a.appName()+"-"+name))
	if err != nil {
		return commandDeps{}, err
	}
	recorder, err := auditpg.NewStore(pool)
	if err != nil {
		pool.Close()
		return commandDeps{}, err
	}
	svc, err := usecase.NewService(usecase.Config{
		Store: repository.NewStore(pool), Catalog: catalog, Recorder: recorder, Emails: noEmails{}, Keyring: keyring(cfg), Issuer: a.appName(),
	})
	if err != nil {
		pool.Close()
		return commandDeps{}, err
	}
	return commandDeps{pool: pool, auth: svc}, nil
}

// noEmails is for commands that never send email.
type noEmails struct{}

func (noEmails) SendVerificationCode(context.Context, string, string, time.Duration) error {
	return nil
}

func (noEmails) SendPasswordResetCode(context.Context, string, string, time.Duration) error {
	return nil
}

func (noEmails) SendAccountExists(context.Context, string) error     { return nil }
func (noEmails) SendPasswordChanged(context.Context, string) error   { return nil }
func (noEmails) SendTwoFactorEnabled(context.Context, string) error  { return nil }
func (noEmails) SendTwoFactorDisabled(context.Context, string) error { return nil }

func (noEmails) SendRecoveryCodeUsed(context.Context, string, int) error { return nil }
func (noEmails) SendPasskeyAdded(context.Context, string, string) error  { return nil }
func (noEmails) SendPasskeyRemoved(context.Context, string, string) error {
	return nil
}

func (noEmails) SendSignInMethodAdded(context.Context, string, string) error   { return nil }
func (noEmails) SendSignInMethodRemoved(context.Context, string, string) error { return nil }
