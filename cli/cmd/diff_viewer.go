package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// resolveDiffCommand returns the external diff command (argv prefix, before the
// two file paths) and whether one is available. Precedence:
//   - GAFFER_EXTERNAL_DIFF (split on spaces) for an explicit override;
//   - `git diff --no-index`, which honours the user's git diff configuration
//     (GIT_EXTERNAL_DIFF, diff.external, pager, colour) where git is installed;
//   - `diff -u` as a last resort.
//
// ok is false when none is available, so the caller can fall back to a hint
// rather than dumping the source.
func resolveDiffCommand(getenv func(string) string, lookPath func(string) (string, error)) (argv []string, ok bool) {
	if custom := strings.TrimSpace(getenv("GAFFER_EXTERNAL_DIFF")); custom != "" {
		return strings.Fields(custom), true
	}
	if _, err := lookPath("git"); err == nil {
		return []string{"git", "diff", "--no-index"}, true
	}
	if _, err := lookPath("diff"); err == nil {
		return []string{"diff", "-u"}, true
	}
	return nil, false
}

// openSourceDiff renders the deployed vs local query through an external diff
// viewer. The two queries are written to temp files (named for readable diff
// headers) and the viewer is run with stdout/stderr inherited so it pages and
// colours itself. With no viewer available it prints a hint instead of dumping
// the source.
func openSourceDiff(name, deployed, local string, out, errOut io.Writer) error {
	argv, ok := resolveDiffCommand(os.Getenv, exec.LookPath)
	if !ok {
		_, _ = fmt.Fprintf(errOut, "%s: query differs - set GAFFER_EXTERNAL_DIFF or install git to view the source diff\n", name)
		return nil
	}

	dir, err := os.MkdirTemp("", "gaffer-diff-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	safe := strings.ReplaceAll(name, string(os.PathSeparator), "_")
	deployedPath := filepath.Join(dir, safe+".deployed")
	localPath := filepath.Join(dir, safe+".local")
	if err := os.WriteFile(deployedPath, []byte(deployed), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(localPath, []byte(local), 0o600); err != nil {
		return err
	}

	args := append(append([]string{}, argv[1:]...), deployedPath, localPath)
	c := exec.Command(argv[0], args...)
	c.Stdout = out
	c.Stderr = errOut
	// git diff --no-index and diff -u exit non-zero when the files differ, which
	// is always the case here, so the exit status isn't a failure to surface.
	_ = c.Run()
	return nil
}
