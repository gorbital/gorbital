package app

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"strings"

	"gorbital.dev/actor"

	authdomain "example.com/acme-api/internal/modules/auth/domain"
	projectsmodule "example.com/acme-api/internal/modules/projects"
	projectsdomain "example.com/acme-api/internal/modules/projects/domain"
	projectsusecase "example.com/acme-api/internal/modules/projects/usecase"
)

// DefaultSeedEmail is the administrator Seed creates unless told otherwise.
const DefaultSeedEmail = "admin@example.com"

// seedProjects are the example projects owned by the seeded administrator.
var seedProjects = []projectsdomain.ProjectFields{
	{Name: "Website redesign", Description: "A new layout and faster pages.", Status: projectsdomain.StatusActive},
	{Name: "Mobile app", Description: "iOS and Android clients for the API.", Status: projectsdomain.StatusActive},
	{Name: "Legacy import", Description: "Moved the old data over; kept for reference.", Status: projectsdomain.StatusArchived},
}

// Seed fills a development database with a platform administrator and
// example projects (ADR-0042). The administrator has two-factor
// authentication on, because its role requires it (ADR-0043): its random
// password, authenticator app key and recovery codes are written to w once
// and stored nowhere in plain text. Seed refuses to run in production and is
// safe to run again: when the administrator exists, it changes nothing.
// Everything goes through the modules' use cases, so the password policy,
// hashing, encryption and audit events apply.
//
//	go run ./cmd/seed
func Seed(ctx context.Context, cfg Config, email string, w io.Writer) error {
	if cfg.Production() {
		return errors.New("seed data is for development only, and APP_ENV is production")
	}
	if cfg.keyring() == nil {
		return errors.New("AUTH_ENCRYPTION_KEYS is required: the administrator's role needs two-factor authentication, whose secrets it encrypts (orb dev sets it in .env)")
	}
	deps, err := openCommandDeps(ctx, cfg, "seed")
	if err != nil {
		return err
	}
	defer deps.pool.Close()
	projects, err := projectsmodule.New(deps.pool, projectsusecase.Config{Recorder: deps.recorder})
	if err != nil {
		return err
	}

	ctx = actor.With(ctx, actor.System("seed"))
	_, err = deps.auth.UserByEmail(ctx, email)
	switch {
	case err == nil:
		fmt.Fprintf(w, "✓ Seed data is in place: %s exists and was left unchanged.\n"+
			"  Lost its password? POST /v1/auth/password/forgot, then use the code from Mailpit.\n", email)
		return nil
	case !errors.Is(err, authdomain.ErrUserNotFound):
		return err
	}

	password := rand.Text()
	admin, err := deps.auth.CreateUser(ctx, email, password, true)
	if err != nil {
		return fmt.Errorf("create the administrator: %w", err)
	}
	if err := deps.auth.GrantRole(ctx, admin.ID, "platform_admin"); err != nil {
		return fmt.Errorf("grant platform_admin to the administrator: %w", err)
	}
	enrollment, recoveryCodes, err := deps.auth.EnrollTOTP(ctx, admin.ID)
	if err != nil {
		return fmt.Errorf("turn on two-factor authentication for the administrator: %w", err)
	}
	// The examples belong to the administrator, as if they had created them.
	asAdmin := actor.With(ctx, actor.Actor{Kind: actor.KindUser, ID: admin.ID, Label: admin.Email})
	for _, p := range seedProjects {
		if _, err := projects.Service().Create(asAdmin, p); err != nil {
			return fmt.Errorf("create the example project %q: %w", p.Name, err)
		}
	}

	half := len(recoveryCodes) / 2
	fmt.Fprintf(w, `✓ Seed data created
  Administrator:   %s (platform_admin)
  Password:        %s
  2FA key:         %s
  2FA QR code URI: %s
  Recovery codes:  %s
                   %s
  Projects:        %d examples owned by the administrator

  These are shown only this once and aren't saved anywhere. Add the 2FA key to
  an authenticator app: signing in as the administrator asks for its code, and
  /ops needs it. Lost the password? POST /v1/auth/password/forgot and the code
  from Mailpit. Lost the authenticator app? Sign in with a recovery code.
`, admin.Email, password, enrollment.Secret, enrollment.URI,
		strings.Join(recoveryCodes[:half], "  "), strings.Join(recoveryCodes[half:], "  "), len(seedProjects))
	return nil
}
