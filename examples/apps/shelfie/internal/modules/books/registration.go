package books

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/gorbital/authhttp"

	"example.com/shelfie/internal/modules/books/repository"
	"example.com/shelfie/internal/modules/books/usecase"
)

// docs:start on-register

// CreateDefaultShelf is an authhttp.OnRegister hook: every new reader,
// whether they registered with an email address, signed in with Google,
// Apple or GitHub for the first time, or were created by an operator,
// starts with a "Reading" shelf. It writes in tx, the transaction that
// creates the account, so the account and its shelf exist together or not
// at all.
func CreateDefaultShelf(ctx context.Context, tx pgx.Tx, a authhttp.NewAccount) error {
	svc := usecase.NewService(repository.NewStore(tx), nil, nil, nil)
	return svc.CreateDefaultShelf(ctx, a.User.ID)
}

// docs:end on-register
