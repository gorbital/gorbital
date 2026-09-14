package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"hash/fnv"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// watchIgnored are directories never watched for changes.
var watchIgnored = map[string]bool{".git": true, ".aps": true, "bin": true, "node_modules": true, "vendor": true, "tmp": true}

func runDev(ctx context.Context, args []string, stderr io.Writer) error {
	flags := flag.NewFlagSet("aps dev", flag.ContinueOnError)
	flags.SetOutput(stderr)
	noReload := flags.Bool("no-reload", false, "build and run once, without watching for changes")
	interval := flags.Duration("interval", 500*time.Millisecond, "how often to check for changes")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return usageError("aps dev takes no arguments")
	}
	if _, err := os.Stat("apistock.yaml"); err != nil {
		return usageError("no apistock.yaml in this directory; run aps dev inside an app created with aps new")
	}

	d := &devRunner{out: stderr, bin: filepath.Join(".aps", "api")}
	return d.loop(ctx, !*noReload, *interval)
}

type devRunner struct {
	out  io.Writer
	bin  string
	cmd  *exec.Cmd
	done chan error
}

func (d *devRunner) loop(ctx context.Context, reload bool, interval time.Duration) error {
	if err := d.build(ctx); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}
	if err := d.start(); err != nil {
		return err
	}
	defer d.stop()

	if !reload {
		select {
		case <-ctx.Done():
			return nil
		case err := <-d.done:
			d.cmd = nil
			return exitError(err)
		}
	}

	last, _ := snapshot(".")
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			fmt.Fprintln(d.out, "aps: stopping")
			return nil
		case err := <-d.done:
			fmt.Fprintf(d.out, "aps: app exited (%v); waiting for changes\n", exitError(err))
			d.cmd, d.done = nil, nil
		case <-ticker.C:
			cur, err := snapshot(".")
			if err != nil || cur == last {
				continue
			}
			last = cur
			fmt.Fprintln(d.out, "aps: change detected, rebuilding")
			if err := d.build(ctx); err != nil {
				if d.cmd != nil {
					fmt.Fprintln(d.out, "aps: build failed; the previous version keeps running")
				} else {
					fmt.Fprintln(d.out, "aps: build failed; fix the errors and save again")
				}
				continue
			}
			d.stop()
			if err := d.start(); err != nil {
				fmt.Fprintf(d.out, "aps: start failed: %v\n", err)
			}
		}
	}
}

func (d *devRunner) build(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "go", "build", "-o", d.bin, "./cmd/api")
	cmd.Stdout, cmd.Stderr = d.out, d.out
	return cmd.Run()
}

func (d *devRunner) start() error {
	env, err := devEnv(".env")
	if err != nil {
		return err
	}
	cmd := exec.Command(d.bin)
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, d.out
	configureProcess(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start app: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	d.cmd, d.done = cmd, done
	return nil
}

// stop asks the app to shut down gracefully and kills it after 10 seconds.
func (d *devRunner) stop() {
	if d.cmd == nil {
		return
	}
	_ = interrupt(d.cmd)
	select {
	case <-d.done:
	case <-time.After(10 * time.Second):
		_ = d.cmd.Process.Kill()
		<-d.done
	}
	d.cmd, d.done = nil, nil
}

func exitError(err error) error {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return fmt.Errorf("exit status %d", ee.ExitCode())
	}
	return err
}

// snapshot fingerprints the watched files: Go sources, module files, .env
// and embedded assets.
func snapshot(dir string) (uint64, error) {
	h := fnv.New64a()
	err := filepath.WalkDir(dir, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if p != dir && watchIgnored[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !watched(entry.Name()) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil // file vanished during the walk
		}
		fmt.Fprintf(h, "%s|%d|%d\n", p, info.ModTime().UnixNano(), info.Size())
		return nil
	})
	return h.Sum64(), err
}

func watched(name string) bool {
	switch {
	case strings.HasSuffix(name, ".go"), name == "go.mod", name == "go.sum", name == ".env":
		return true
	case strings.HasSuffix(name, ".html"), strings.HasSuffix(name, ".json"), strings.HasSuffix(name, ".sql"):
		return true
	}
	return false
}
