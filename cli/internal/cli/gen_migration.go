package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/huh"
)

const genMigrationUsage = `Usage: aps gen migration <name> [flags]

Creates an empty SQL migration in db/migrations that runs after every existing
one. Write the change under "-- +goose Up", then run go run ./cmd/migrate.
Migrations only go forward: once one is released, change the schema with a
new migration instead of editing it. Run it inside an app created with the
Full preset.

The name says what the migration changes, for example:
  aps gen migration add_customer_phone
  aps gen migration AddCustomerPhone      (same file name)
`

type genMigrationResult struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	File    string `json:"file"`
	DryRun  bool   `json:"dry_run"`
}

func runGenMigration(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("aps gen migration", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dryRun := flags.Bool("dry-run", false, "show what would be generated without writing")
	asJSON := flags.Bool("json", false, "print the result as JSON")
	allowDirty := flags.Bool("allow-dirty", false, "allow uncommitted changes in the git repository")
	var p promptFlags
	flags.BoolVar(&p.yes, "yes", false, "use defaults for everything not given by flags, without prompts")
	flags.BoolVar(&p.noInput, "no-input", false, "never prompt; fail if a required value is missing")
	flags.BoolVar(&p.plain, "plain", false, "plain line-by-line prompts (screen-reader friendly)")
	flags.Usage = func() {
		fmt.Fprint(stderr, genMigrationUsage+"\nFlags:\n")
		flags.PrintDefaults()
	}

	positional, err := parseInterspersed(flags, args)
	if err != nil {
		return err
	}
	if len(positional) > 1 {
		return usageError(fmt.Sprintf("aps gen migration takes one name, got %d: join the words with underscores, such as add_customer_phone", len(positional)))
	}
	var name string
	if len(positional) == 1 {
		name = positional[0]
	}

	app, err := findApp()
	if err != nil {
		return err
	}
	if name == "" && shouldPrompt(p, *asJSON, stdin, stdout) {
		input := huh.NewInput().Title("Migration name").
			Description("What the migration changes, such as add_customer_phone.").
			Placeholder("add_customer_phone").Value(&name).
			Validate(func(s string) error { _, err := migrationName(s); return err })
		if err := runForm(huh.NewForm(huh.NewGroup(input)), p, stdin, stderr); err != nil {
			return err
		}
	}
	if name == "" {
		return usageError("missing migration name: aps gen migration <name> (or run it in a terminal to be asked)")
	}
	words, err := migrationName(name)
	if err != nil {
		return usageError(err.Error())
	}
	version, err := nextMigrationVersion(app.dir, time.Now())
	if err != nil {
		return err
	}
	result := genMigrationResult{
		Name:    strings.Join(words, "_"),
		Version: version,
		File:    "db/migrations/" + version + "_" + strings.Join(words, "_") + ".sql",
		DryRun:  *dryRun,
	}

	if !*dryRun {
		if !*allowDirty {
			if err := requireCleanGit(ctx, app.dir); err != nil {
				return err
			}
		}
		if err := writeNewFile(app.dir, result.File, migrationContent(words)); err != nil {
			return err
		}
	}

	if *asJSON {
		return writeJSON(stdout, result)
	}
	verb := "Created"
	if *dryRun {
		verb = "Would create (dry run)"
	}
	fmt.Fprintf(stdout, "✓ %s migration %s\n", verb, result.File)
	if !*dryRun {
		fmt.Fprint(stdout, "\nNext:\n  1. Write the SQL under -- +goose Up\n  2. go run ./cmd/migrate\n  3. go test ./...\n\n"+
			"Write the SQL before migrating: an empty migration is recorded as applied, and\n"+
			"SQL added to it afterwards never runs. Change the migration freely until it is\n"+
			"released; afterwards, add a new one.\n")
	}
	return nil
}

// migrationName splits a name such as add_customer_phone, AddCustomerPhone or
// add-customer-phone into lowercase words.
func migrationName(input string) ([]string, error) {
	input = strings.TrimSpace(input)
	if len(input) == 0 || len(input) > 60 || !jobNamePattern.MatchString(input) {
		return nil, fmt.Errorf("migration name must start with a letter and use letters, digits, hyphens or underscores (max 60), such as add_customer_phone")
	}
	return splitWords(input), nil
}

// splitWords splits a name such as CleanupSessions, cleanup-sessions or
// cleanup_sessions into lowercase words: at hyphens and underscores, and
// where a capital letter starts a new word.
func splitWords(input string) []string {
	var words []string
	var word []rune
	runes := []rune(input)
	flush := func() {
		if len(word) > 0 {
			words = append(words, strings.ToLower(string(word)))
			word = word[:0]
		}
	}
	for i, r := range runes {
		switch {
		case r == '_' || r == '-':
			flush()
			continue
		case unicode.IsUpper(r) && i > 0:
			prev := runes[i-1]
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && nextLower) {
				flush()
			}
		}
		word = append(word, r)
	}
	flush()
	return words
}

// migrationContent is a new migration: a description and an empty Up
// section. There is no Down section: migrations only go forward (ADR-0005).
// Goose reads every comment line containing its marker as an annotation, so
// only the Up line may mention it.
func migrationContent(words []string) []byte {
	sentence := strings.Join(words, " ")
	sentence = strings.ToUpper(sentence[:1]) + sentence[1:]
	return []byte("-- " + sentence + ".\n" +
		"--\n" +
		"-- Change this migration freely until it is released; afterwards, add a new\n" +
		"-- one.\n" +
		"\n" +
		"-- +goose Up\n")
}

// writeNewFile creates path inside dir with content, failing if the file
// already exists.
func writeNewFile(dir, path string, content []byte) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	f, err := root.OpenFile(filepath.FromSlash(path), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(content); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
