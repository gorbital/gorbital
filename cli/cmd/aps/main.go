// The aps command creates and runs apistock applications.
//
// Usage:
//
//	aps new <name> [flags]   create an application
//	aps dev [flags]          run the application with live reload
//	aps version              print version information
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"apistock.dev/cli/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli.Main(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
