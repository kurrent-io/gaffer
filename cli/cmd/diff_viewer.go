package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// externalDiffCommand returns the user's opt-in external diff command (the
// argv prefix, before the two file paths), from GAFFER_EXTERNAL_DIFF split on
// spaces. The default query diff renders in-process (WriteQueryDiff); the
// external viewer is the escape hatch for tools like delta or difftastic.
func externalDiffCommand(getenv func(string) string) (argv []string, ok bool) {
	if custom := strings.TrimSpace(getenv("GAFFER_EXTERNAL_DIFF")); custom != "" {
		return strings.Fields(custom), true
	}
	return nil, false
}

// openSourceDiff renders the left vs right query through the user's external
// diff viewer. The two queries are written to temp files (suffixed with each
// side's label for readable diff headers) and the viewer is run with
// stdout/stderr inherited so it pages and colours itself.
func openSourceDiff(argv []string, name, leftLabel, leftQuery, rightLabel, rightQuery string, out, errOut io.Writer) error {
	dir, err := os.MkdirTemp("", "gaffer-diff-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	safe := strings.ReplaceAll(name, string(os.PathSeparator), "_")
	leftPath := filepath.Join(dir, safe+"."+leftLabel)
	rightPath := filepath.Join(dir, safe+"."+rightLabel)
	if err := os.WriteFile(leftPath, []byte(leftQuery), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(rightPath, []byte(rightQuery), 0o600); err != nil {
		return err
	}

	args := append(append([]string{}, argv[1:]...), leftPath, rightPath)
	c := exec.Command(argv[0], args...) //nolint:gosec // argv is the user's configured diff command
	c.Stdout = out
	c.Stderr = errOut
	// Exit 1 is the diff convention for "files differ" (git diff --no-index,
	// diff -u, delta), which is always the case here, so it isn't a failure.
	// Anything else non-zero is the tool reporting real trouble (POSIX diff uses
	// 2, git uses higher codes for usage errors), and a start failure (a
	// misconfigured GAFFER_EXTERNAL_DIFF, a missing binary) is surfaced as-is.
	err = c.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &exitErr) && exitErr.ExitCode() == 1:
		return nil
	case errors.As(err, &exitErr):
		return fmt.Errorf("diff viewer %q exited with status %d", argv[0], exitErr.ExitCode())
	default:
		return fmt.Errorf("running diff viewer %q: %w", argv[0], err)
	}
}
