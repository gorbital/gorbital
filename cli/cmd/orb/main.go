// The orb command creates and runs gorbital applications.
//
// Usage:
//
//	orb new [<name>] [flags]      create an application
//	orb gen job [<Name>] [flags]  generate a background job
//	orb dev [flags]               run the application with live reload
//	orb version                   print version information
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"gorbital.dev/cli/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli.Main(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
