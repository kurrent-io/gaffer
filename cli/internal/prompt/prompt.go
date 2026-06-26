// Package prompt wraps huh for the CLI's interactive mode. Commands
// gate every prompt on Enabled so piped/CI invocations and explicit
// --yes always take the non-interactive path and never block on stdin.
package prompt

import (
	"errors"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/kurrent-io/gaffer/cli/internal/ttyutil"
)

// ErrCancelled is returned when the user aborts a prompt (Ctrl+C / Esc).
// It's a package-local sentinel so commands can detect a clean
// cancellation - and exit quietly rather than printing an error banner -
// without importing huh. Check with errors.Is.
var ErrCancelled = errors.New("cancelled")

// Enabled reports whether interactive prompting should run: the user did
// not pass --yes, and both stdin and stderr are terminals. stdin carries
// keystrokes; huh renders to stderr (its default output), so a redirected
// stderr (`2>file`) has nowhere to draw and must fall back to
// non-interactive. A single gate so the rule stays identical across
// commands.
//
// The contract this expresses: --yes (and any non-terminal, e.g. pipes /
// CI) means "take the non-interactive path." It does not mean "accept
// defaults" - whether the non-interactive path has enough to proceed or
// errors (e.g. a still-missing required positional) is each command's
// decision, not this gate's.
func Enabled(yes bool) bool {
	return !yes &&
		ttyutil.IsTerminal(os.Stdin) &&
		ttyutil.IsTerminal(os.Stderr)
}

// Option pairs a display label with the value returned when chosen.
type Option struct {
	Label string
	Value string
}

// Opt is a label==value option, the common case.
func Opt(value string) Option { return Option{Label: value, Value: value} }

// Input prompts for free text, pre-filled with value. placeholder is the
// greyed-out hint shown while the field is empty ("" for none). validate
// may be nil; when set it runs on each submit and a non-nil error keeps
// the user on the field.
func Input(title, value, placeholder string, validate func(string) error) (string, error) {
	v := value
	field := huh.NewInput().Title(title).Placeholder(placeholder).Value(&v)
	if validate != nil {
		field = field.Validate(validate)
	}
	if err := run(field); err != nil {
		return "", err
	}
	return v, nil
}

// Select prompts to choose one option, with value pre-highlighted.
func Select(title string, options []Option, value string) (string, error) {
	v := value
	opts := make([]huh.Option[string], len(options))
	for i, o := range options {
		opts[i] = huh.NewOption(o.Label, o.Value)
	}
	if err := run(huh.NewSelect[string]().Title(title).Options(opts...).Value(&v)); err != nil {
		return "", err
	}
	return v, nil
}

// Confirm prompts for a yes/no, defaulting to value. Used both for
// boolean fields and the summary confirm before an action commits.
func Confirm(title string, value bool) (bool, error) {
	v := value
	if err := run(huh.NewConfirm().Title(title).Value(&v)); err != nil {
		return false, err
	}
	return v, nil
}

// run wraps a single field in a one-group form themed to match the
// rest of the charm-based CLI output (fang, lipgloss). A user abort is
// translated to ErrCancelled so callers get one sentinel regardless of
// which prompt they were on.
func run(field huh.Field) error {
	err := huh.NewForm(huh.NewGroup(field)).WithTheme(huh.ThemeCharm()).Run()
	if errors.Is(err, huh.ErrUserAborted) {
		return ErrCancelled
	}
	return err
}
