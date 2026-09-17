package main

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/gorbital/authhttp"

	"example.com/shelfie/internal/modules/phonelogin"
	"example.com/shelfie/internal/modules/profiles"
	shelves "example.com/shelfie/internal/modules/shelves/domain"
)

// docs:start sign-in-options

// signInOptions are how Shelfie's sign-in differs from the library's
// defaults (chapter 6).
func signInOptions() []authhttp.Option {
	return []authhttp.Option{
		// POST /v1/auth/register also takes display_name and country, and
		// saves them in the transaction that creates the account.
		authhttp.RegisterFields(profiles.SaveRegistration),
		// Every new account starts with a shelf, however it was created.
		authhttp.OnRegister(createDefaultShelf),
		// Suspended readers can't sign in, whichever way they try.
		authhttp.BeforeLogin(profiles.RefuseSuspended),
		// Readers' passwords are at least 14 characters.
		authhttp.MinPasswordLength(14),
	}
}

// docs:end sign-in-options

// docs:start default-shelf

// defaultShelfName names the shelf every new reader starts with.
const defaultShelfName = "Reading"

// createDefaultShelf is an authhttp.OnRegister hook: every new reader,
// whether they registered with an email address, signed in with Google,
// Apple or GitHub for the first time, or were created by an operator,
// starts with a private "Reading" shelf. It writes in tx, the transaction
// that creates the account, so the account and its shelf exist together or
// not at all.
//
// The shelves module's domain makes the shelf, with its rules and defaults.
// The module's store runs on the pool (its InTx begins a transaction of its
// own), so the hook inserts the row on tx, into the columns the shelves
// migration defines.
func createDefaultShelf(ctx context.Context, tx pgx.Tx, a authhttp.NewAccount) error {
	now := time.Now().UTC().Truncate(time.Microsecond) // PostgreSQL's precision
	shelf, err := shelves.NewShelf(newShelfID(), a.User.ID,
		shelves.ShelfFields{Name: defaultShelfName, Visibility: shelves.VisibilityPrivate}, now)
	if err != nil {
		return fmt.Errorf("shelfie: default shelf: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO shelves (id, owner_id, name, description, visibility, version, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		shelf.ID, shelf.OwnerID, shelf.Name, shelf.Description, shelf.Visibility, shelf.Version, shelf.CreatedAt, shelf.UpdatedAt)
	if err != nil {
		return fmt.Errorf("shelfie: create the default shelf: %w", err)
	}
	return nil
}

// newShelfID returns "shl_" and 128 random bits, as the shelves module's
// IDs are.
func newShelfID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // never fails (crypto/rand)
	return "shl_" + base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding).EncodeToString(b)
}

// docs:end default-shelf

// docs:start sms-sender

// smsSender texts phone sign-in codes (chapter 7). Shelfie has no SMS
// provider yet: in development codes go to the log, and production answers
// 503 sms_unavailable until a real sender replaces this one.
func smsSender() phonelogin.Sender {
	return phonelogin.LogSender{Production: os.Getenv("APP_ENV") == "production"}
}

// docs:end sms-sender
