package jobs

import (
	"embed"
	"io/fs"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrations holds the goose migrations for job definition overrides and
// their history. Apps copy them into db/migrations; River's own tables come
// from [Migrate].
var Migrations fs.FS = mustSub(migrationFiles, "migrations")

func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return sub
}
