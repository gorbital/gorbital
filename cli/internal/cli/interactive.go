package cli

import (
	"errors"
	"io"
	"os"

	"github.com/charmbracelet/huh"
	"golang.org/x/term"
)

// errAborted reports that the user cancelled a prompt (exit code 130).
var errAborted = errors.New("aborted, nothing was written")

// promptFlags are the interaction flags every prompting command accepts
// (ADR-0035).
type promptFlags struct {
	yes     bool
	noInput bool
	plain   bool
}

// shouldPrompt reports whether a command may ask questions: only on a
// terminal, and never with --yes, --json, --no-input or in CI.
func shouldPrompt(p promptFlags, asJSON bool, stdin io.Reader, stdout io.Writer) bool {
	if p.yes || p.noInput || asJSON || os.Getenv("CI") != "" {
		return false
	}
	in, inOK := stdin.(*os.File)
	out, outOK := stdout.(*os.File)
	return inOK && outOK && term.IsTerminal(int(in.Fd())) && term.IsTerminal(int(out.Fd())) //nolint:gosec // file descriptors fit in int
}

// runForm shows form on stderr. --plain or ACCESSIBLE=1 selects the
// screen-reader friendly line-by-line mode.
func runForm(form *huh.Form, p promptFlags, stdin io.Reader, stderr io.Writer) error {
	accessible := p.plain || os.Getenv("ACCESSIBLE") != ""
	err := form.WithAccessible(accessible).WithInput(stdin).WithOutput(stderr).Run()
	if errors.Is(err, huh.ErrUserAborted) {
		return errAborted
	}
	return err
}

// confirm asks a yes/no question, defaulting to yes.
func confirm(title, summary string, p promptFlags, stdin io.Reader, stderr io.Writer) (bool, error) {
	ok := true
	form := huh.NewForm(huh.NewGroup(
		huh.NewNote().Title("Summary").Description(summary),
		huh.NewConfirm().Title(title).Affirmative("Yes").Negative("No").Value(&ok),
	))
	if err := runForm(form, p, stdin, stderr); err != nil {
		return false, err
	}
	return ok, nil
}
