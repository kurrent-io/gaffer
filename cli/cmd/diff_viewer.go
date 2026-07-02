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

// openSourceDiff renders the remote (deployed) vs local query through the
// user's external diff viewer. The two queries are written to temp files
// (named for readable diff headers) and the viewer is run with stdout/stderr
// inherited so it pages and colours itself.
func openSourceDiff(argv []string, name, remoteQuery, local string, out, errOut io.Writer) error {
	dir, err := os.MkdirTemp("", "gaffer-diff-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	safe := strings.ReplaceAll(name, string(os.PathSeparator), "_")
	remotePath := filepath.Join(dir, safe+".remote")
	localPath := filepath.Join(dir, safe+".local")
	if err := os.WriteFile(remotePath, []byte(remoteQuery), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(localPath, []byte(local), 0o600); err != nil {
		return err
	}

	args := append(append([]string{}, argv[1:]...), remotePath, localPath)
	c := exec.Command(argv[0], args...) //nolint:gosec // argv is the user's configured diff command
	c.Stdout = out
	c.Stderr = errOut
	// A diff tool that ran and exited non-zero is reporting "files differ" (git
	// diff --no-index and diff -u use exit 1), which is always the case here, so
	// the exit status isn't a failure. A start failure (a misconfigured
	// GAFFER_EXTERNAL_DIFF, a missing binary) is real and surfaced.
	err = c.Run()
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		return fmt.Errorf("running diff viewer %q: %w", argv[0], err)
	}
	return nil
}
