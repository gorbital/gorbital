package cli

import (
	"io"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// The gorbital palette (docs/brand/theme.md). Lime marks one thing per
// screen: the open question, or the command to run next.
const (
	colorAccent = lipgloss.Color("#d8ff3e")
	colorInk    = lipgloss.Color("#f0efe9")
	colorMuted  = lipgloss.Color("#a5a59a")
	colorDim    = lipgloss.Color("#8a8a80")
	colorDanger = lipgloss.Color("#ff8f6b")
)

// styles renders output for one writer. Colour follows that writer: none
// when it isn't a terminal or NO_COLOR is set, so the text reads the same
// without it.
type styles struct {
	accent, ink, strong, muted, dim lipgloss.Style
}

func newStyles(w io.Writer) styles {
	r := lipgloss.NewRenderer(w)
	return styles{
		accent: r.NewStyle().Foreground(colorAccent),
		ink:    r.NewStyle().Foreground(colorInk),
		strong: r.NewStyle().Foreground(colorInk).Bold(true),
		muted:  r.NewStyle().Foreground(colorMuted),
		dim:    r.NewStyle().Foreground(colorDim),
	}
}

// question is the title of an open question: "? app name … ".
func (s styles) question(label string) string {
	return s.accent.Render("?") + " " + s.strong.Render(label) + s.dim.Render(" … ")
}

// choice is the title of a question answered from a list: "? preset › ↑↓ move, enter choose".
func (s styles) choice(label string) string {
	return s.accent.Render("?") + " " + s.strong.Render(label) + s.dim.Render(" ›  ↑↓ move, enter choose")
}

// answered is the line an answered question folds into: "✓ app name … shop-api".
func (s styles) answered(label, answer string) string {
	return s.muted.Render("✓") + " " + s.ink.Render(label) + s.dim.Render(" … ") + s.ink.Render(answer)
}

// theme is the huh theme for every prompt: no borders, dim hints, a lime
// cursor, and the chosen option in ink.
func theme() *huh.Theme {
	t := huh.ThemeBase()

	t.Focused.Base = lipgloss.NewStyle()
	t.Focused.Card = t.Focused.Base
	t.Focused.Title = lipgloss.NewStyle().Foreground(colorInk).Bold(true)
	t.Focused.NoteTitle = t.Focused.Title.MarginBottom(1)
	t.Focused.Description = lipgloss.NewStyle().Foreground(colorDim)
	t.Focused.ErrorIndicator = lipgloss.NewStyle().Foreground(colorDanger).SetString(" !")
	t.Focused.ErrorMessage = lipgloss.NewStyle().Foreground(colorDanger)
	t.Focused.SelectSelector = lipgloss.NewStyle().Foreground(colorAccent).SetString("❯ ")
	t.Focused.NextIndicator = lipgloss.NewStyle().Foreground(colorAccent).MarginLeft(1).SetString("→")
	t.Focused.PrevIndicator = lipgloss.NewStyle().Foreground(colorAccent).MarginRight(1).SetString("←")
	t.Focused.Option = lipgloss.NewStyle().Foreground(colorMuted)
	t.Focused.SelectedOption = lipgloss.NewStyle().Foreground(colorInk).Bold(true)
	t.Focused.UnselectedOption = lipgloss.NewStyle().Foreground(colorMuted)
	t.Focused.MultiSelectSelector = t.Focused.SelectSelector
	t.Focused.SelectedPrefix = lipgloss.NewStyle().Foreground(colorAccent).SetString("[x] ")
	t.Focused.UnselectedPrefix = lipgloss.NewStyle().Foreground(colorDim).SetString("[ ] ")
	t.Focused.FocusedButton = lipgloss.NewStyle().Foreground(colorInk).Bold(true).Underline(true).MarginRight(1)
	t.Focused.BlurredButton = lipgloss.NewStyle().Foreground(colorDim).MarginRight(1)
	t.Focused.Next = t.Focused.FocusedButton
	t.Focused.TextInput.Cursor = lipgloss.NewStyle().Foreground(colorAccent)
	t.Focused.TextInput.Placeholder = lipgloss.NewStyle().Foreground(colorDim)
	t.Focused.TextInput.Prompt = lipgloss.NewStyle().Foreground(colorAccent)
	t.Focused.TextInput.Text = lipgloss.NewStyle().Foreground(colorInk)

	t.Blurred = t.Focused
	t.Blurred.SelectSelector = lipgloss.NewStyle().SetString("  ")
	t.Blurred.MultiSelectSelector = t.Blurred.SelectSelector
	t.Blurred.NextIndicator = lipgloss.NewStyle()
	t.Blurred.PrevIndicator = lipgloss.NewStyle()

	t.Group.Title = t.Focused.Title
	t.Group.Description = t.Focused.Description
	t.Help.ShortKey = lipgloss.NewStyle().Foreground(colorDim)
	t.Help.ShortDesc = lipgloss.NewStyle().Foreground(colorDim)
	t.Help.ShortSeparator = lipgloss.NewStyle().Foreground(colorDim)
	return t
}
