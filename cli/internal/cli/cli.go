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
  aps new <name> [flags]   create an application
  aps dev [flags]          run the application with live reload
  aps version              print version information
  aps help                 show this help

Run "aps <command> -h" for a command's flags.
`

// usageError is an error in how aps was invoked (exit code 2).
type usageError string

func (e usageError) Error() string { return string(e) }

// Main runs aps with args and returns the process exit code: 0 on success,
// 1 on failure, 2 on invalid usage.
func Main(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}

	var err error
	switch args[0] {
	case "new":
		err = runNew(ctx, args[1:], stdout, stderr)
	case "dev":
		err = runDev(ctx, args[1:], stderr)
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
	}
	fmt.Fprintf(stderr, "aps: %v\n", err)
	var ue usageError
	if errors.As(err, &ue) {
		return 2
	}
	return 1
}
