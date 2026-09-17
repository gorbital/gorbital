// Package cli implements the orb commands.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"

	"gorbital.dev/cli/internal/recipes"
)

// Version is the orb version. Release builds set it with
// -ldflags "-X gorbital.dev/cli/internal/cli.Version=v0.1.0"; go install
// gorbital.dev/cli/cmd/orb@v0.1.0 gets it from the module version.
var Version = moduleVersion("v0.1.0-dev", debug.ReadBuildInfo)

// cliModulePath is the module orb is built from.
const cliModulePath = "gorbital.dev/cli"

// releaseVersion matches a tagged module version: vX.Y.Z with an optional
// pre-release, but not a pseudo-version or a build of a modified tree.
var releaseVersion = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$`)

var pseudoVersion = regexp.MustCompile(`[0-9]{14}-[0-9a-f]{12}$`)

// moduleVersion returns the version of the gorbital.dev/cli module orb was
// installed at, when the build info has one, and fallback otherwise: for
// builds from a checkout, which report (devel) or a pseudo-version.
func moduleVersion(fallback string, read func() (*debug.BuildInfo, bool)) string {
	info, ok := read()
	if !ok || info.Main.Path != cliModulePath {
		return fallback
	}
	v := info.Main.Version
	if !releaseVersion.MatchString(v) || pseudoVersion.MatchString(v) {
		return fallback
	}
	return v
}

const usage = `orb creates and runs gorbital applications.

Usage:
  orb new [<name>] [flags]       create an application
  orb gen job [<Name>] [flags]   generate a background job (Full preset apps)
  orb gen resource [<Name> <field:type>...] [flags]
                                 generate a module, table and API for users' records (Full preset apps)
  orb gen migration [<name>] [flags]
                                 generate an empty database migration (Full preset apps)
  orb gen module [<Name> <field:type>...] [flags]
                                 generate a layered module with its table and API (apps using gorbital.Main)
  orb gen middleware <Name> [flags]
                                 generate middleware or a guard with its test (apps using gorbital.Main)
  orb gen modules [flags]        list internal/modules in modules.gen.go (apps using gorbital.Main)
  orb routes [flags]             list every route: guards, public routes, handlers and source
  orb eject <module> [flags]     copy a built-in module (auth, flags, mailevents, ops, orgs) into the app
                                 as code it owns (apps using gorbital.Main)
  orb add mail [flags]           set up email with Resend or SMTP (Full preset apps)
  orb add orgs [flags]           turn a single-tenant app multi-tenant on a branch (Full preset apps)
  orb add rls [flags]            turn on row-level security for organisations' data (multi-tenant apps)
  orb dev [flags]                run the application with live reload and the Dev Portal
  orb upgrade [flags]            merge this release's templates into the app on a branch
  orb doctor [flags]             check the app, its environment and database, and say what to fix
  orb version [--json]           print version information
  orb help                       show this help

In a terminal, commands ask for anything you leave out, with arrow-key menus.
Every question has a flag; pass --yes to accept defaults without questions.
Run "orb <command> -h" for a command's flags.
`

// minimumGo is the oldest Go release orb should be built with. orb writes
// apps' files through os.Root so no path or symlink escapes the app (ADR-0029,
// threat 3); Go 1.26.5 is the first 1.26 release with every os.Root escape
// fixed (GO-2026-4602, GO-2026-4864, GO-2026-4970). go.mod keeps go 1.26.0
// (ADR-0015), so go install accepts older toolchains: orb version and orb
// doctor warn instead.
const minimumGo = "1.26.5"

// toolchainWarning returns a warning when goVersion, as runtime.Version
// reports it, is a Go release older than minimumGo, and "" otherwise,
// including for development toolchains.
func toolchainWarning(goVersion string) string {
	release, _, _ := strings.Cut(goVersion, " ")
	have, ok := strings.CutPrefix(release, "go")
	if !ok || versionAtLeast(have, minimumGo) {
		return ""
	}
	return fmt.Sprintf("orb was built with %s, which lacks security fixes orb relies on; reinstall it with Go %s or newer (the latest patch release)", release, minimumGo)
}

// usageError is an error in how orb was invoked (exit code 2).
type usageError string

func (e usageError) Error() string { return string(e) }

// versionResult is orb version --json.
type versionResult struct {
	Version string `json:"version"`
	Recipe  string `json:"recipe"`
	Library string `json:"library"`
}

func runVersion(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("orb version", flag.ContinueOnError)
	flags.SetOutput(stderr)
	asJSON := flags.Bool("json", false, "print the result as JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return usageError(fmt.Sprintf("unexpected arguments: %s", strings.Join(flags.Args(), " ")))
	}
	if warning := toolchainWarning(runtime.Version()); warning != "" {
		fmt.Fprintf(stderr, "orb: warning: %s\n", warning)
	}
	if *asJSON {
		return writeJSON(stdout, versionResult{Version: Version, Recipe: recipes.MinimalName, Library: recipes.LibraryVersion})
	}
	fmt.Fprintf(stdout, "orb %s (recipe %s, library %s)\n", Version, recipes.MinimalName, recipes.LibraryVersion)
	return nil
}

// Main runs orb with args and returns the process exit code: 0 on success,
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
		err = runUpgrade(ctx, args[1:], stdin, stdout, stderr)
	case "routes":
		err = runRoutes(ctx, args[1:], stdout, stderr)
	case "eject":
		err = runEject(ctx, args[1:], stdin, stdout, stderr)
	case "doctor":
		err = runDoctor(ctx, args[1:], stdout, stderr)
	case "version", "-version", "--version":
		err = runVersion(args[1:], stdout, stderr)
	case "help", "-h", "-help", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "orb: unknown command %q\n\n%s", args[0], usage)
		return 2
	}

	switch {
	case err == nil:
		return 0
	case errors.Is(err, flag.ErrHelp):
		return 0
	case errors.Is(err, errAborted):
		fmt.Fprintf(stderr, "orb: %v\n", err)
		return 130
	}
	fmt.Fprintf(stderr, "orb: %v\n", err)
	var ue usageError
	if errors.As(err, &ue) {
		return 2
	}
	return 1
}
