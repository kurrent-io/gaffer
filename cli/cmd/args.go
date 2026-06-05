package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// argCountError is returned by exactArgs when a command gets the wrong
// number of positional arguments. It carries the command so errorHandler
// can name the argument (from the Use line) and show a runnable example -
// neither of which cobra's stock "Accepts 1 arg(s), received 0." conveys.
type argCountError struct {
	cmd  *cobra.Command
	got  int
	want int
}

func (e *argCountError) Error() string {
	placeholder := argPlaceholder(e.cmd)

	var b strings.Builder
	switch {
	case placeholder == "":
		fmt.Fprintf(&b, "expected %d argument(s), got %d", e.want, e.got)
	case e.got > e.want:
		fmt.Fprintf(&b, "too many arguments: %s takes %s", e.cmd.CommandPath(), placeholder)
	default:
		fmt.Fprintf(&b, "missing required argument %s", placeholder)
	}
	if e.cmd.Example != "" {
		fmt.Fprintf(&b, "\nexample: %s", e.cmd.Example)
	}
	return b.String()
}

// argPlaceholder returns the positional-argument portion of a command's
// Use line - "<path>" from "scaffold <path>". Empty when the Use line is
// just the command name.
func argPlaceholder(cmd *cobra.Command) string {
	fields := strings.Fields(cmd.Use)
	if len(fields) < 2 {
		return ""
	}
	return strings.Join(fields[1:], " ")
}

// exactArgs requires exactly n positional arguments, returning an
// argCountError on mismatch so the user sees the argument name and an
// example instead of cobra's bare count message. Used in place of
// cobra.ExactArgs on commands with a single required positional whose Use
// line names the argument; the message reads best for that case.
func exactArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) == n {
			return nil
		}
		return &argCountError{cmd: cmd, got: len(args), want: n}
	}
}

// maxArgs allows up to n positional arguments, returning an argCountError
// (the same styled message as exactArgs) when too many are given. Used by
// commands whose single positional is required only on the non-interactive
// path: the positional may be omitted to prompt for it, so the missing
// case is enforced in RunE via missingArgErr rather than here.
func maxArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) <= n {
			return nil
		}
		return &argCountError{cmd: cmd, got: len(args), want: n}
	}
}

// missingArgErr is the error a command raises from RunE when its required
// positional was omitted on the non-interactive path. Mirrors
// exactArgs(1)'s "missing required argument <name>" message so relaxing a
// command to maxArgs(1) doesn't change what non-interactive users see.
func missingArgErr(cmd *cobra.Command) error {
	return &argCountError{cmd: cmd, got: 0, want: 1}
}
