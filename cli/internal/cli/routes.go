package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"gorbital.dev/cli/internal/routes"
)

const routesUsage = `Usage: orb routes [flags]

Lists every route of the app in the current directory: method, path,
operation ID, module, guards, middleware, handler and where it is registered
(file:line). Public routes, which need no sign-in, say public in GUARDS.

The routes come from the app's OpenAPI document, built with
go run ./cmd/api openapi (or read with --openapi), joined with the app's Go
source. Guards come from x-gorbital-guards, so an app on the v0.1 layout
lists none; middleware is what gorbital.Use and Module.Middleware name in the
source. It changes nothing.
`

// routesExport builds the app's OpenAPI document. Tests replace it.
var routesExport = routes.Export

func runRoutes(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("orb routes", flag.ContinueOnError)
	flags.SetOutput(stderr)
	asJSON := flags.Bool("json", false, "print the routes as JSON")
	file := flags.String("openapi", "", "read this OpenAPI document, such as api/openapi.json, instead of building the app")
	module := flags.String("module", "", "only the routes of this module")
	public := flags.Bool("public", false, "only public routes (no sign-in required)")
	flags.Bool("no-input", false, "never prompt (orb routes never does)")
	flags.Usage = func() {
		fmt.Fprint(stderr, routesUsage+"\nFlags:\n")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return usageError(fmt.Sprintf("unexpected arguments: %s", strings.Join(flags.Args(), " ")))
	}
	app, err := findApp()
	if err != nil {
		return err
	}
	list, err := appRoutes(ctx, app.dir, *file)
	if err != nil {
		return err
	}
	list = list.Filter(*module, *public)
	if *asJSON {
		return writeJSON(stdout, list)
	}
	writeRoutes(stdout, list)
	return nil
}

// appRoutes returns the routes of the app in dir, from the OpenAPI document
// in file, or built from the app when file is empty.
func appRoutes(ctx context.Context, dir, file string) (routes.List, error) {
	var doc []byte
	source := routes.SourceFile
	if file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return routes.List{}, err
		}
		doc = data
	} else {
		env, err := devEnv(filepath.Join(dir, envPath))
		if err != nil {
			return routes.List{}, err
		}
		if doc, err = routesExport(ctx, dir, withAppEnv(env)); err != nil {
			return routes.List{}, fmt.Errorf("couldn't build the OpenAPI document: %w\n  check that the app builds (go build ./...), or pass --openapi api/openapi.json", err)
		}
		source = routes.SourceExport
	}
	return routes.Build(filepath.Base(dir), dir, doc, source)
}

// writeRoutes prints routes as a table, then the counts and warnings.
func writeRoutes(w io.Writer, l routes.List) {
	withMiddleware := false
	for _, r := range l.Routes {
		withMiddleware = withMiddleware || len(r.Middleware) > 0
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	header := "METHOD\tPATH\tOPERATION\tMODULE\tGUARDS\t"
	if withMiddleware {
		header += "MIDDLEWARE\t"
	}
	fmt.Fprintln(tw, header+"HANDLER\tSOURCE")
	for _, r := range l.Routes {
		guards := strings.Join(r.Guards, ", ")
		switch {
		case guards != "":
		case r.Public:
			guards = "public"
		case !l.GuardsKnown:
			guards = "?"
		}
		line := fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t", r.Method, r.Path, r.OperationID, dash(r.Module), dash(guards))
		if withMiddleware {
			line += dash(strings.Join(r.Middleware, ", ")) + "\t"
		}
		fmt.Fprintln(tw, line+dash(r.Handler)+"\t"+dash(r.Source.String()))
	}
	_ = tw.Flush()
	noun := "routes"
	if l.Total == 1 {
		noun = "route"
	}
	fmt.Fprintf(w, "\n%d %s, %d public\n", l.Total, noun, l.Public)
	for _, warning := range l.Warnings {
		fmt.Fprintf(w, "note: %s\n", warning)
	}
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
