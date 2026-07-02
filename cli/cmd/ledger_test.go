package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initGitRepo makes a temp git repo with one empty commit, isolated from the
// machine's global/system git config. Skips if git isn't on PATH.
func initGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", dir}, args...)...)
		c.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "--allow-empty", "-q", "-m", "init")
	return dir
}

func TestGitRevisionClean(t *testing.T) {
	dir := initGitRepo(t)
	rev := gitRevision(dir)
	if len(rev) != 40 {
		t.Fatalf("revision = %q, want a 40-char HEAD SHA", rev)
	}
	if strings.Contains(rev, "+changes") {
		t.Errorf("clean tree must not be marked dirty: %q", rev)
	}
	want, _ := git(dir, "rev-parse", "HEAD")
	if rev != want {
		t.Errorf("revision = %q, want HEAD %q", rev, want)
	}
}

func TestGitRevisionDirty(t *testing.T) {
	dir := initGitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "untracked.js"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	rev := gitRevision(dir)
	if !strings.HasSuffix(rev, "+changes") {
		t.Errorf("revision = %q, want +changes suffix for an untracked file", rev)
	}
}

func TestGitRevisionNotARepo(t *testing.T) {
	if rev := gitRevision(t.TempDir()); rev != "" {
		t.Errorf("revision = %q, want empty outside a git repo", rev)
	}
}

func TestResolveRevision(t *testing.T) {
	dir := initGitRepo(t)

	t.Run("env overrides git", func(t *testing.T) {
		t.Setenv("GAFFER_REVISION", "from-env")
		if got := resolveRevision(dir); got != "from-env" {
			t.Errorf("got %q, want from-env", got)
		}
	})
	t.Run("git when no override", func(t *testing.T) {
		t.Setenv("GAFFER_REVISION", "")
		if got := resolveRevision(dir); len(got) != 40 {
			t.Errorf("got %q, want the git HEAD", got)
		}
	})
}

func TestResolveActorEnvOverride(t *testing.T) {
	// GAFFER_ACTOR overrides the connection-derived principal. (The derived path is
	// covered by engine.Principal's own tests.)
	t.Setenv("GAFFER_ACTOR", "ci-bot")
	if got := resolveActor("", "", nil, ""); got != "ci-bot" {
		t.Errorf("got %q, want ci-bot", got)
	}
}
