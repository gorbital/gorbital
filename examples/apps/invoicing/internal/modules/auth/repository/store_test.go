package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorbital.dev/modules/postgres/pgtest"

	authdomain "example.com/invoicing/internal/modules/auth/domain"
	authrepository "example.com/invoicing/internal/modules/auth/repository"
	"example.com/invoicing/internal/modules/auth/repository/migrations"
	authusecase "example.com/invoicing/internal/modules/auth/usecase"
)

func newStore(t *testing.T) *authrepository.Store {
	t.Helper()
	return authrepository.NewStore(pgtest.New(t, pgtest.WithMigrations(migrations.FS)))
}

func TestUsersAndRoles(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	in := authdomain.User{ID: "usr_1", Email: "Ada@Example.com", NormalizedEmail: "ada@example.com", PasswordHash: "$argon2id$hash", CreatedAt: now}

	created, err := store.InsertUser(ctx, in)
	if err != nil || created.ID != "usr_1" || created.EmailVerified() || !created.CreatedAt.Equal(now) {
		t.Fatalf("InsertUser() = %+v, %v", created, err)
	}
	if _, err := store.InsertUser(ctx, authdomain.User{ID: "usr_2", Email: "ada@example.com", NormalizedEmail: "ada@example.com", CreatedAt: now}); !errors.Is(err, authdomain.ErrEmailTaken) {
		t.Errorf("InsertUser(taken address) error = %v, want ErrEmailTaken", err)
	}
	if u, err := store.SelectUserByEmail(ctx, "ada@example.com", false); err != nil || u.PasswordHash != "$argon2id$hash" {
		t.Errorf("SelectUserByEmail() = %+v, %v", u, err)
	}
	if _, err := store.SelectUserByID(ctx, "usr_missing", false); !errors.Is(err, authdomain.ErrUserNotFound) {
		t.Errorf("SelectUserByID(missing) error = %v, want ErrUserNotFound", err)
	}

	if err := store.MarkEmailVerified(ctx, "usr_1", now); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := store.InsertUserRole(ctx, "usr_1", "viewer", "system:test", now); err != nil {
			t.Fatal(err)
		}
	}
	if roles, err := store.SelectUserRoles(ctx, "usr_1"); err != nil || len(roles) != 1 {
		t.Errorf("SelectUserRoles() = %v, %v", roles, err)
	}

	if err := store.MarkUserDeleted(ctx, "usr_1", now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SelectUserByEmail(ctx, "ada@example.com", false); !errors.Is(err, authdomain.ErrUserNotFound) {
		t.Errorf("deleted account selected: %v", err)
	}
	if n, err := store.DeleteDeletedUsers(ctx, now.Add(time.Second)); err != nil || n != 1 {
		t.Errorf("DeleteDeletedUsers() = %d, %v", n, err)
	}
}

func TestInTxRollsBack(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()
	errFail := errors.New("fail")
	err := store.InTx(ctx, func(tx authusecase.Store) error {
		if _, err := tx.InsertUser(ctx, authdomain.User{ID: "usr_1", Email: "a@example.com", NormalizedEmail: "a@example.com", CreatedAt: time.Now()}); err != nil {
			return err
		}
		return errFail
	})
	if !errors.Is(err, errFail) {
		t.Fatalf("InTx() error = %v", err)
	}
	if _, err := store.SelectUserByID(ctx, "usr_1", false); !errors.Is(err, authdomain.ErrUserNotFound) {
		t.Errorf("rolled back insert is visible: %v", err)
	}
}
