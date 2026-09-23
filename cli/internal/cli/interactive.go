package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
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

// plainPrompts reports whether prompts use the screen-reader friendly
// line-by-line mode: --plain or ACCESSIBLE=1.
func plainPrompts(p promptFlags) bool {
	return p.plain || os.Getenv("ACCESSIBLE") != ""
}

// runForm shows form on stderr in the gorbital theme, or line by line in
// plain mode.
func runForm(form *huh.Form, p promptFlags, stdin io.Reader, stderr io.Writer) error {
	err := form.WithTheme(theme()).WithLayout(insetLayout{huh.LayoutDefault}).
		WithAccessible(plainPrompts(p)).WithInput(stdin).WithOutput(stderr).Run()
	if errors.Is(err, huh.ErrUserAborted) {
		return errAborted
	}
	return err
}

// promptInset is how many columns a prompt leaves free at the right edge of
// the terminal. huh pads every line to the width it is given; a line that
// fills the last column wraps as soon as the terminal draws one character a
// column wider than counted (… and ↑↓ are ambiguous-width, two columns in
// some terminals), and every redraw then leaves a copy of the question on
// screen.
const promptInset = 3

// insetLayout is a huh layout whose groups stay promptInset columns inside
// the terminal, following it when the window is resized.
type insetLayout struct{ huh.Layout }

func (l insetLayout) GroupWidth(f *huh.Form, g *huh.Group, w int) int {
	return l.Layout.GroupWidth(f, g, max(w-promptInset, 1))
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

// asker asks one question at a time. Each answered question folds into a
// single line, so every answer stays on screen while the next is asked. In
// plain mode questions are plain lines and nothing is folded: the answers
// are already printed.
type asker struct {
	p      promptFlags
	stdin  io.Reader
	stderr io.Writer
	plain  bool
	s      styles
}

func newAsker(p promptFlags, stdin io.Reader, stderr io.Writer) asker {
	return asker{p: p, stdin: stdin, stderr: stderr, plain: plainPrompts(p), s: newStyles(stderr)}
}

// title is the title of a text or yes/no question.
func (a asker) title(label string) string {
	if a.plain {
		return label
	}
	return a.s.question(label)
}

// choiceTitle is the title of a question answered by choosing an option.
func (a asker) choiceTitle(label string) string {
	if a.plain {
		return label
	}
	return a.s.choice(label)
}

// ask runs field on its own, then folds it into "✓ label … answer".
func (a asker) ask(field huh.Field, label string, answer func() string) error {
	if err := runForm(huh.NewForm(huh.NewGroup(field)).WithShowHelp(false), a.p, a.stdin, a.stderr); err != nil {
		return err
	}
	a.answered(label, answer())
	return nil
}

// answered prints the folded line for a value, also for values given by
// flag, so the whole set of answers is visible before anything is written.
func (a asker) answered(label, answer string) {
	if !a.plain {
		fmt.Fprintln(a.stderr, a.s.answered(label, answer))
	}
}

// yesNo is a yes/no question on one line, its answers right after the title.
func (a asker) yesNo(label string, value *bool) *huh.Confirm {
	return huh.NewConfirm().Title(a.title(label)).Inline(true).
		Affirmative("yes").Negative("no").WithButtonAlignment(lipgloss.Left).Value(value)
}

// confirm asks a yes/no question on one line, defaulting to yes.
func (a asker) confirm(label string) (bool, error) {
	ok := true
	err := a.ask(a.yesNo(label, &ok), label, func() string { return yesNo(ok) })
	return ok, err
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
