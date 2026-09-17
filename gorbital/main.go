package gorbital

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"gorbital.dev/buildinfo"
	"gorbital.dev/config"
)

// ErrUsage marks an error in how a command was called, such as a missing
// argument. [Main] exits with status 2 for it; wrap it in a [Command]'s
// errors: fmt.Errorf("%w: grant-role <email> <role>", gorbital.ErrUsage).
var ErrUsage = errors.New("usage")

// A Command is a subcommand of [Main] beside the built-in ones. A value
// passed to [WithAuth] that has a method Commands() []Command contributes
// its commands, as the built-in sign-in does for its role commands.
type Command struct {
	// Name is what follows the program name, such as "grant-role". It
	// can't be a built-in command's name.
	Name string
	// Usage is one line: the arguments, then what the command does, such as
	// "grant-role <email> <role>   give an account a platform role".
	Usage string
	// Run runs the command with the loaded configuration and the arguments
	// after the name. Return an error wrapping [ErrUsage] for bad
	// arguments.
	Run func(ctx context.Context, cfg Config, args []string, stdout io.Writer) error
}

// builtinCommands are Main's own commands, in the order usage lists them.
var builtinCommands = []string{"serve", "migrate", "migrate-down", "openapi", "version", "help"}

// Main runs the app as a command-line program, for main.go:
//
//	func main() {
//		gorbital.Main(
//			gorbital.WithModules(modules.All()...),
//			gorbital.WithMigrations(migrations.FS),
//		)
//	}
//
// The first argument chooses the command:
//
//	serve (default)             load the configuration from the environment, build the app with New and Run it
//	migrate [--status [--json]] apply pending migrations (Migrate), or report them and change nothing
//	migrate-down                roll back the most recent migration; development only
//	openapi [--dir <dir>]       print the OpenAPI document, built without a database, or write it
//	                            with a Postman collection and llms.txt into dir, which
//	                            is created when it doesn't exist
//	version [--json]            print the build's version, commit and Go version
//
// Main never returns: it exits with status 0 on success, 1 on a runtime
// error, and 2 for a usage or configuration error, such as an unknown
// command or an invalid environment variable. Errors go to standard error,
// prefixed with the app's name.
func Main(opts ...Option) {
	os.Exit(run(context.Background(), os.Args[1:], config.OS, os.Stdout, os.Stderr, opts))
}

// run is Main without the exit, for tests.
func run(ctx context.Context, args []string, src config.Source, stdout, stderr io.Writer, opts []Option) int {
	o := newOptions(opts)
	commands, err := o.commandList()
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", o.name, err)
		return 2
	}
	name := "serve"
	if len(args) > 0 {
		name, args = args[0], args[1:]
	}
	err = runCommand(ctx, name, args, src, stdout, stderr, o, commands)
	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
		return 0
	case errors.Is(err, errInvalidConfig), errors.Is(err, ErrUsage):
		fmt.Fprintf(stderr, "%s: %v\n", o.name, err)
		return 2
	default:
		fmt.Fprintf(stderr, "%s: %v\n", o.name, err)
		return 1
	}
}

func runCommand(ctx context.Context, name string, args []string, src config.Source, stdout, stderr io.Writer, o options, commands []Command) error {
	flags := flag.NewFlagSet(o.name+" "+name, flag.ContinueOnError)
	flags.SetOutput(stderr)
	usageErr := func() error {
		flags.Usage()
		return fmt.Errorf("%w: %s", ErrUsage, strings.TrimSpace(usageLine(name, commands)))
	}
	// parse parses the command's flags, defined before it is called, and
	// refuses positional arguments; -h returns flag.ErrHelp (exit 0).
	parse := func() error {
		if err := flags.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return err
			}
			return usageErr()
		}
		if flags.NArg() > 0 {
			return usageErr()
		}
		return nil
	}
	switch name {
	case "help", "-h", "-help", "--help":
		fmt.Fprint(stdout, usage(o.name, commands))
		return nil

	case "serve":
		if err := parse(); err != nil {
			return err
		}
		cfg, err := LoadConfig(src)
		if err != nil {
			return err
		}
		a, err := New(ctx, cfg, withOptions(o))
		if err != nil {
			return err
		}
		return a.Run(ctx)

	case "migrate":
		status := flags.Bool("status", false, "report pending migrations without applying them")
		asJSON := flags.Bool("json", false, "with --status, print one JSON object")
		if err := parse(); err != nil {
			return err
		}
		if *asJSON && !*status {
			return usageErr()
		}
		cfg, cfgErr := LoadConfig(src)
		if *status {
			s := readMigrationStatus(ctx, cfg, cfgErr, o)
			if *asJSON {
				return json.NewEncoder(stdout).Encode(s)
			}
			switch {
			case s.ConfigError != "":
				return fmt.Errorf("%w: %s", errInvalidConfig, s.ConfigError)
			case s.DatabaseError != "":
				return errors.New(s.DatabaseError)
			}
			fmt.Fprintf(stdout, "migrations: database at %d, newest file %d, %d pending\n", s.Current, s.Latest, s.Pending)
			for _, warning := range s.RowLevelSecurity {
				fmt.Fprintf(stdout, "warning: %s\n", warning)
			}
			return nil
		}
		if cfgErr != nil {
			return cfgErr
		}
		return Migrate(ctx, cfg, stdout, withOptions(o))

	case "migrate-down":
		if err := parse(); err != nil {
			return err
		}
		cfg, err := LoadConfig(src)
		if err != nil {
			return err
		}
		return migrateDown(ctx, cfg, stdout, o)

	case "openapi":
		dir := flags.String("dir", "", "write openapi.json, postman_collection.json and llms.txt into this directory")
		if err := parse(); err != nil {
			return err
		}
		if *dir != "" {
			return writeAPIFiles(*dir, o)
		}
		return writeOpenAPI(stdout, o)

	case "version":
		asJSON := flags.Bool("json", false, "print one JSON object")
		if err := parse(); err != nil {
			return err
		}
		info := buildinfo.Read()
		if *asJSON {
			return json.NewEncoder(stdout).Encode(info)
		}
		fmt.Fprintf(stdout, "%s %s", o.name, info.Version)
		if info.Commit != "" {
			fmt.Fprintf(stdout, " (%s)", info.Commit)
		}
		fmt.Fprintf(stdout, " %s\n", info.GoVersion)
		return nil
	}

	for _, c := range commands {
		if c.Name == name {
			cfg, err := LoadConfig(src)
			if err != nil {
				return err
			}
			if err := o.setupAuthForCommand(ctx, cfg); err != nil {
				return err
			}
			return c.Run(ctx, cfg, args, stdout)
		}
	}
	fmt.Fprint(stderr, usage(o.name, commands))
	return fmt.Errorf("%w: unknown command %q", ErrUsage, name)
}

// withOptions passes already applied options on.
func withOptions(o options) Option {
	return optionFunc(func(dst *options) { *dst = o })
}

// commandList returns the commands the authenticator contributes, checked
// against the built-in names and each other.
func (o options) commandList() ([]Command, error) {
	p, ok := o.auth.(interface{ Commands() []Command })
	if !ok {
		return nil, nil
	}
	commands := p.Commands()
	seen := map[string]bool{}
	for _, c := range commands {
		switch {
		case c.Name == "" || strings.HasPrefix(c.Name, "-") || strings.ContainsAny(c.Name, " \t\n"):
			return nil, fmt.Errorf("gorbital: command name %q must be one word", c.Name)
		case slices.Contains(builtinCommands, c.Name):
			return nil, fmt.Errorf("gorbital: command %q is built in", c.Name)
		case seen[c.Name]:
			return nil, fmt.Errorf("gorbital: command %q is defined twice", c.Name)
		case c.Run == nil:
			return nil, fmt.Errorf("gorbital: command %q has no Run", c.Name)
		}
		seen[c.Name] = true
	}
	return commands, nil
}

func usage(name string, commands []Command) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Usage: %s [command]\n\nCommands:\n", name)
	for _, c := range append(slices.Clone(builtinCommands[:5]), commandNames(commands)...) {
		b.WriteString("  " + usageLine(c, commands) + "\n")
	}
	return b.String()
}

func commandNames(commands []Command) []string {
	names := make([]string, 0, len(commands))
	for _, c := range commands {
		names = append(names, c.Name)
	}
	return names
}

func usageLine(name string, commands []Command) string {
	switch name {
	case "serve":
		return "serve                          run the API server (the default)"
	case "migrate":
		return "migrate [--status [--json]]    apply pending migrations, or report them"
	case "migrate-down":
		return "migrate-down                   roll back the most recent migration (development only)"
	case "openapi":
		return "openapi [--dir <directory>]    print the OpenAPI document, or write it with the Postman collection and llms.txt"
	case "version":
		return "version [--json]               print the build's version"
	}
	for _, c := range commands {
		if c.Name == name {
			return c.Usage
		}
	}
	return name
}
