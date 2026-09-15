// Package cli implements the aps commands.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"apistock.dev/cli/internal/recipes"
)

// Version is the aps version. Release builds set it with
// -ldflags "-X apistock.dev/cli/internal/cli.Version=v0.1.0".
var Version = "v0.1.0-dev"

const usage = `aps creates and runs apistock applications.

Usage:
  aps new [<name>] [flags]       create an application
  aps gen job [<Name>] [flags]   generate a background job (Full preset apps)
  aps gen resource [<Name> <field:type>...] [flags]
                                 generate a module, table and API for users' records (Full preset apps)
  aps gen migration [<name>] [flags]
                                 generate an empty database migration (Full preset apps)
  aps add mail [flags]           set up email with Resend or SMTP (Full preset apps)
  aps add orgs [flags]           turn a single-tenant app multi-tenant on a branch (Full preset apps)
  aps dev [flags]                run the application with live reload
  aps upgrade [flags]            merge this release's templates into the app on a branch
  aps doctor [flags]             check the app, its environment and database, and say what to fix
  aps version                   print version information
  aps help                       show this help

In a terminal, commands ask for anything you leave out, with arrow-key menus.
Every question has a flag; pass --yes to accept defaults without questions.
Run "aps <command> -h" for a command's flags.
`

// usageError is an error in how aps was invoked (exit code 2).
type usageError string

func (e usageError) Error() string { return string(e) }

// Main runs aps with args and returns the process exit code: 0 on success,
// 1 on failure, 2 on invalid usage, 130 when the user cancels a prompt.
// Prompts read stdin and draw on stderr, only when both are terminals.
func Main(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}

	var err error
	switch args[0] {
	case "new":
		err = runNew(ctx, args[1:], stdin, stdout, stderr)
	case "gen":
		err = runGen(ctx, args[1:], stdin, stdout, stderr)
	case "add":
		err = runAdd(ctx, args[1:], stdin, stdout, stderr)
	case "dev":
		err = runDev(ctx, args[1:], stderr)
	case "upgrade":
		err = runUpgrade(ctx, args[1:], stdout, stderr)
	case "doctor":
		err = runDoctor(ctx, args[1:], stdout, stderr)
	case "version", "-version", "--version":
		fmt.Fprintf(stdout, "aps %s (recipe %s, library %s)\n", Version, recipes.MinimalName, recipes.LibraryVersion)
		return 0
	case "help", "-h", "-help", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "aps: unknown command %q\n\n%s", args[0], usage)
		return 2
	}

	switch {
	case err == nil:
		return 0
	case errors.Is(err, flag.ErrHelp):
		return 0
	case errors.Is(err, errAborted):
		fmt.Fprintf(stderr, "aps: %v\n", err)
		return 130
	}
	fmt.Fprintf(stderr, "aps: %v\n", err)
	var ue usageError
	if errors.As(err, &ue) {
		return 2
	}
	return 1
}
