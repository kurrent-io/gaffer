package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/fang"
)

// requiredArgCommands are the leaf commands that take exactly one
// required positional. Each must use exactArgs so a missing/extra arg
// names the argument and shows an example.
var requiredArgCommands = []struct {
	args        []string // path to the command under root
	placeholder string   // expected positional placeholder in the Use line
	prompts     bool     // omitting the positional prompts on a TTY, so it's
	// required only non-interactively (maxArgs + missingArgErr from RunE)
	// rather than rejected at the Args layer (exactArgs).
}{
	{[]string{"scaffold"}, "<path>", true},
	{[]string{"dev"}, "<projection>", false},
	{[]string{"info"}, "<projection>", false},
}

func TestExactArgs_MissingArgNamesArgumentAndExample(t *testing.T) {
	for _, tc := range requiredArgCommands {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			cmd := findLeaf(t, NewRootCmd(), tc.args)

			// Where the missing-positional error is raised differs:
			// exactArgs commands reject at the Args layer; prompt commands
			// allow zero args there (so a bare TTY invocation can prompt)
			// and raise missingArgErr from RunE on the non-interactive
			// path. Both must produce the same styled message.
			var err error
			if tc.prompts {
				if argsErr := cmd.Args(cmd, nil); argsErr != nil {
					t.Fatalf("prompt command should allow zero args at the Args layer, got %v", argsErr)
				}
				err = missingArgErr(cmd)
			} else {
				err = cmd.Args(cmd, nil)
			}
			if err == nil {
				t.Fatal("expected an error for zero args")
			}
			var argErr *argCountError
			if !errors.As(err, &argErr) {
				t.Fatalf("expected *argCountError, got %T", err)
			}

			msg := err.Error()
			if !strings.Contains(msg, "missing required argument "+tc.placeholder) {
				t.Errorf("message should name the argument %q, got:\n%s", tc.placeholder, msg)
			}
			if cmd.Example == "" {
				t.Fatal("command should set Example so the error can show one")
			}
			if !strings.Contains(msg, cmd.Example) {
				t.Errorf("message should include the example %q, got:\n%s", cmd.Example, msg)
			}
		})
	}
}

func TestExactArgs_TooManyArgs(t *testing.T) {
	cmd := findLeaf(t, NewRootCmd(), []string{"scaffold"})
	err := cmd.Args(cmd, []string{"a.js", "b.js"})
	if err == nil {
		t.Fatal("expected an error for extra args")
	}
	if !strings.Contains(err.Error(), "too many arguments") {
		t.Errorf("expected a too-many-arguments message, got:\n%s", err.Error())
	}
}

func TestExactArgs_CorrectCountPasses(t *testing.T) {
	cmd := findLeaf(t, NewRootCmd(), []string{"scaffold"})
	if err := cmd.Args(cmd, []string{"order.js"}); err != nil {
		t.Errorf("expected no error for exactly one arg, got %v", err)
	}
}

// errorHandler must keep the headline and example on separate lines.
// Rendering through fang's width-reflowing ErrorText style collapses the
// newline into a space, joining them - the reason errorHandler prints the
// body itself.
func TestErrorHandler_KeepsExampleOnItsOwnLine(t *testing.T) {
	cmd := findLeaf(t, NewRootCmd(), []string{"scaffold"})
	var buf bytes.Buffer
	errorHandler(&buf, fang.Styles{}, &argCountError{cmd: cmd, got: 0, want: 1})

	out := buf.String()
	if !strings.Contains(out, "missing required argument <path>") {
		t.Errorf("expected the headline, got:\n%s", out)
	}
	if !strings.Contains(out, "\n  example: gaffer scaffold ./projections/order.js") {
		t.Errorf("expected the example on its own indented line, got:\n%s", out)
	}
}

// Required positionals use <...> (angle = required); [...] (square =
// optional) would misdescribe a command that errors without the arg.
func TestRequiredPositionalsUseAngleBrackets(t *testing.T) {
	for _, tc := range requiredArgCommands {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			cmd := findLeaf(t, NewRootCmd(), tc.args)
			if strings.ContainsAny(cmd.Use, "[]") {
				t.Errorf("Use %q should not use [optional] brackets for a required arg", cmd.Use)
			}
			if !strings.Contains(cmd.Use, "<") {
				t.Errorf("Use %q should declare the required arg with <...>", cmd.Use)
			}
		})
	}
}
