// The orb command is a first-run spike prototype: `orb new <name>` renders
// the Minimal template, and `orb dev` builds and runs the generated app.
// Throwaway code: the real CLI is designed in ADR-0021.
package main

import (
	"embed"
	"errors"
	"flag"
	"fmt"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
)

// Template files end in .tmpl so an embedded go.mod doesn't turn the
// template directory into a separate (unembeddable) module.
//
//go:embed all:template
var templates embed.FS

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "orb:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: orb new <name> [-module path] | orb dev")
	}
	switch args[0] {
	case "new":
		return newApp(args[1:])
	case "dev":
		return dev()
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

type data struct {
	Name   string
	Module string
}

func newApp(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: orb new <name> [-module path]")
	}
	name := args[0]
	flags := flag.NewFlagSet("new", flag.ContinueOnError)
	module := flags.String("module", name, "Go module path")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if !namePattern.MatchString(name) {
		return fmt.Errorf("invalid app name %q: use lowercase letters, digits and hyphens", name)
	}
	for _, part := range strings.Split(*module, "/") {
		if !token.IsIdentifier(strings.NewReplacer("-", "_", ".", "_").Replace(part)) {
			return fmt.Errorf("invalid module path %q", *module)
		}
	}
	if _, err := os.Stat(name); err == nil {
		return fmt.Errorf("%s already exists", name)
	}
	if err := os.Mkdir(name, 0o755); err != nil {
		return err
	}
	root, err := os.OpenRoot(name)
	if err != nil {
		return err
	}
	defer root.Close()

	d := data{Name: name, Module: *module}
	const base = "template/minimal"
	err = fs.WalkDir(templates, base, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(p, base), "/")
		if rel == "" {
			return nil
		}
		if e.IsDir() {
			return root.Mkdir(rel, 0o755)
		}
		src, err := templates.ReadFile(p)
		if err != nil {
			return err
		}
		t, err := template.New(rel).Option("missingkey=error").Parse(string(src))
		if err != nil {
			return fmt.Errorf("parse template %s: %w", rel, err)
		}
		f, err := root.Create(strings.TrimSuffix(rel, ".tmpl"))
		if err != nil {
			return err
		}
		if err := t.Execute(f, d); err != nil {
			f.Close()
			return fmt.Errorf("render %s: %w", rel, err)
		}
		return f.Close()
	})
	if err != nil {
		return err
	}

	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir, tidy.Stdout, tidy.Stderr = name, os.Stderr, os.Stderr
	if err := tidy.Run(); err != nil {
		return fmt.Errorf("go mod tidy: %w", err)
	}
	fmt.Printf("✓ created %s\n  next: cd %s && orb dev\n", name, name)
	return nil
}

func dev() error {
	bin := filepath.Join(".orb", "api")
	build := exec.Command("go", "build", "-o", bin, "./cmd/api")
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		return fmt.Errorf("build: %w", err)
	}
	app := exec.Command(bin)
	app.Stdin, app.Stdout, app.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := app.Start(); err != nil {
		return err
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	go func() {
		<-sig
		_ = app.Process.Signal(os.Interrupt)
	}()
	return app.Wait()
}
