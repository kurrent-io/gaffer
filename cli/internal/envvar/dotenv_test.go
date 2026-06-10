package envvar

import (
	"os"
	"path/filepath"
	"testing"
)

// clearEnv unsets the given keys and restores their original values
// after the test, so .env-loading tests don't leak into each other or
// the process.
func clearEnv(t *testing.T, keys ...string) {
	t.Helper()
	for _, key := range keys {
		orig, set := os.LookupEnv(key)
		_ = os.Unsetenv(key)
		if set {
			t.Cleanup(func() { _ = os.Setenv(key, orig) })
		} else {
			t.Cleanup(func() { _ = os.Unsetenv(key) })
		}
	}
}

func TestLoad_NoProjectDir(t *testing.T) {
	if err := Load(""); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestLoad_NoEnvFile(t *testing.T) {
	if err := Load(t.TempDir()); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestLoad_BaseEnvFile(t *testing.T) {
	clearEnv(t, "KURRENTDB_USERNAME", "KURRENTDB_PASSWORD")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("KURRENTDB_USERNAME=admin\nKURRENTDB_PASSWORD=changeit\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Load(dir); err != nil {
		t.Fatal(err)
	}

	user, pass := Credentials(nil)
	if user != "admin" || pass != "changeit" {
		t.Fatalf("expected admin/changeit, got %q/%q", user, pass)
	}
}

// Load is no-override: a value already in the process environment (the
// shell) wins over .env.
func TestLoad_ShellWins(t *testing.T) {
	clearEnv(t, "KURRENTDB_USERNAME")
	t.Setenv("KURRENTDB_USERNAME", "shelluser")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("KURRENTDB_USERNAME=fileuser\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Load(dir); err != nil {
		t.Fatal(err)
	}

	if user, _ := Credentials(nil); user != "shelluser" {
		t.Fatalf("expected shell value to win, got %q", user)
	}
}

func TestCredentials_Empty(t *testing.T) {
	clearEnv(t, "KURRENTDB_USERNAME", "KURRENTDB_PASSWORD")
	if user, pass := Credentials(nil); user != "" || pass != "" {
		t.Fatalf("expected empty credentials, got %q/%q", user, pass)
	}
}

// Per-environment credentials live in .env.<env> and are applied for the
// selected env - the whole point of the per-env file.
func TestCredentials_FromEnvOverlay(t *testing.T) {
	resetSnapshot(t)
	clearEnv(t, "KURRENTDB_USERNAME", "KURRENTDB_PASSWORD")
	dir := t.TempDir()
	Snapshot() // shell layer has no KURRENTDB_* credentials
	writeFile(t, dir, ".env.prod", "KURRENTDB_USERNAME=produser\nKURRENTDB_PASSWORD=prodpass\n")

	user, pass := Credentials(mustOverlay(t, dir, "prod"))
	if user != "produser" || pass != "prodpass" {
		t.Fatalf("expected produser/prodpass from .env.prod, got %q/%q", user, pass)
	}
}

// A real shell credential wins over .env.<env>.
func TestCredentials_ShellOverridesEnvOverlay(t *testing.T) {
	resetSnapshot(t)
	clearEnv(t, "KURRENTDB_USERNAME")
	t.Setenv("KURRENTDB_USERNAME", "shelluser")
	dir := t.TempDir()
	Snapshot() // shell layer includes KURRENTDB_USERNAME=shelluser
	writeFile(t, dir, ".env.prod", "KURRENTDB_USERNAME=produser\n")

	if user, _ := Credentials(mustOverlay(t, dir, "prod")); user != "shelluser" {
		t.Fatalf("expected shell to win over .env.prod, got %q", user)
	}
}

// With no env name (ad-hoc --connection), the overlay isn't consulted.
func TestCredentials_NoEnvNameSkipsOverlay(t *testing.T) {
	resetSnapshot(t)
	clearEnv(t, "KURRENTDB_USERNAME")
	dir := t.TempDir()
	Snapshot()
	writeFile(t, dir, ".env.prod", "KURRENTDB_USERNAME=produser\n")

	if user, _ := Credentials(mustOverlay(t, dir, "")); user != "" {
		t.Fatalf("expected no credential without an env name, got %q", user)
	}
}

func TestExpand_Substitutes(t *testing.T) {
	clearEnv(t, "GAFFER_TEST_SECRET")
	t.Setenv("GAFFER_TEST_SECRET", "s3cr3t")
	got, err := Expand("kurrentdb://admin:${GAFFER_TEST_SECRET}@host:2113", nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := "kurrentdb://admin:s3cr3t@host:2113"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestExpand_UndefinedErrors(t *testing.T) {
	clearEnv(t, "GAFFER_TEST_MISSING")
	_, err := Expand("kurrentdb://${GAFFER_TEST_MISSING}@host", nil)
	if err == nil {
		t.Fatal("expected error for undefined variable")
	}
	if got := err.Error(); !contains(got, "GAFFER_TEST_MISSING") {
		t.Errorf("expected error to name the variable, got %q", got)
	}
}

// A bare `$` (e.g. in an inline password) is not interpolation and is
// left untouched.
func TestExpand_LeavesBareDollarAlone(t *testing.T) {
	in := "kurrentdb://admin:pa$$word@host:2113"
	got, err := Expand(in, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != in {
		t.Fatalf("expected bare $ untouched, got %q", got)
	}
}

func TestLoad_MalformedReturnsError(t *testing.T) {
	dir := t.TempDir()
	// Unterminated double-quote: godotenv rejects it. Guards the
	// doc-promised error path that Connect surfaces and startup swallows.
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("KEY=\"unterminated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Load(dir); err == nil {
		t.Fatal("expected error for malformed .env")
	}
}

func TestExpand_MultipleVars(t *testing.T) {
	clearEnv(t, "GAFFER_TEST_A", "GAFFER_TEST_B")
	t.Setenv("GAFFER_TEST_A", "alpha")
	t.Setenv("GAFFER_TEST_B", "beta")
	got, err := Expand("${GAFFER_TEST_A}-${GAFFER_TEST_B}", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "alpha-beta" {
		t.Fatalf("got %q, want %q", got, "alpha-beta")
	}
}

func TestExpand_NoVarsPassthrough(t *testing.T) {
	in := "kurrentdb://host:2113?tls=false"
	got, err := Expand(in, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != in {
		t.Fatalf("got %q, want unchanged %q", got, in)
	}
}

// ${} has no variable name; the regex requires at least one character,
// so it is left literal rather than treated as a (missing) variable.
func TestExpand_EmptyBracesLeftLiteral(t *testing.T) {
	in := "a${}b"
	got, err := Expand(in, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != in {
		t.Fatalf("got %q, want unchanged %q", got, in)
	}
}

// A substituted value is inserted verbatim - no re-expansion, and a `}`
// or `$` in the value doesn't terminate or re-trigger interpolation.
func TestExpand_ValueWithSpecialCharsInsertedVerbatim(t *testing.T) {
	clearEnv(t, "GAFFER_TEST_VAL")
	t.Setenv("GAFFER_TEST_VAL", "pa}ss$x")
	got, err := Expand("p:${GAFFER_TEST_VAL}@host", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "p:pa}ss$x@host" {
		t.Fatalf("got %q, want verbatim insertion", got)
	}
}

// resetSnapshot clears the package shell snapshot after a test, so a
// test's Snapshot() call doesn't leak into a later test that doesn't
// take its own.
func resetSnapshot(t *testing.T) { t.Cleanup(func() { shellEnv = nil }) }

// .env.<env> overrides the base .env for the selected environment.
func TestExpand_EnvOverlayOverridesBase(t *testing.T) {
	resetSnapshot(t)
	clearEnv(t, "GAFFER_LAYER")
	dir := t.TempDir()
	Snapshot() // shell layer: no GAFFER_LAYER
	writeFile(t, dir, ".env", "GAFFER_LAYER=base\n")
	if err := Load(dir); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, ".env.prod", "GAFFER_LAYER=overlay\n")

	got, err := Expand("${GAFFER_LAYER}", mustOverlay(t, dir, "prod"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "overlay" {
		t.Fatalf("got %q, want overlay (.env.<env> over base .env)", got)
	}
}

// A real shell variable wins over .env.<env>.
func TestExpand_ShellOverridesEnvOverlay(t *testing.T) {
	resetSnapshot(t)
	clearEnv(t, "GAFFER_LAYER")
	t.Setenv("GAFFER_LAYER", "shell")
	dir := t.TempDir()
	Snapshot() // shell layer includes GAFFER_LAYER=shell
	writeFile(t, dir, ".env.prod", "GAFFER_LAYER=overlay\n")

	got, err := Expand("${GAFFER_LAYER}", mustOverlay(t, dir, "prod"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "shell" {
		t.Fatalf("got %q, want shell (shell wins over .env.<env>)", got)
	}
}

// .env.<env> supplies a value the base .env doesn't have.
func TestExpand_EnvOverlayWhenBaseAbsent(t *testing.T) {
	resetSnapshot(t)
	clearEnv(t, "GAFFER_LAYER")
	dir := t.TempDir()
	Snapshot()
	writeFile(t, dir, ".env.prod", "GAFFER_LAYER=overlay\n")

	got, err := Expand("${GAFFER_LAYER}", mustOverlay(t, dir, "prod"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "overlay" {
		t.Fatalf("got %q, want overlay", got)
	}
}

// With no env name, the .env.<env> overlay is not consulted - only the
// base .env (in the process env) applies.
func TestExpand_NoEnvNameSkipsOverlay(t *testing.T) {
	resetSnapshot(t)
	clearEnv(t, "GAFFER_LAYER")
	dir := t.TempDir()
	Snapshot()
	writeFile(t, dir, ".env", "GAFFER_LAYER=base\n")
	if err := Load(dir); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, ".env.prod", "GAFFER_LAYER=overlay\n")

	got, err := Expand("${GAFFER_LAYER}", mustOverlay(t, dir, ""))
	if err != nil {
		t.Fatal(err)
	}
	if got != "base" {
		t.Fatalf("got %q, want base (overlay skipped without env name)", got)
	}
}

// A malformed .env.<env> is surfaced (naming the file) but its contents
// are never echoed - godotenv parse errors include raw file bytes, which
// for a credential file would leak secrets.
func TestOverlay_MalformedErrors(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".env.prod", "PASSWORD=s3cr3t\nKEY=\"unterminated\n")

	_, err := Overlay(dir, "prod")
	if err == nil {
		t.Fatal("expected error for malformed .env.prod")
	}
	if !contains(err.Error(), ".env.prod") {
		t.Errorf("error should name the file, got %q", err.Error())
	}
	if contains(err.Error(), "s3cr3t") || contains(err.Error(), "unterminated") {
		t.Errorf("error leaked file content: %q", err.Error())
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustOverlay(t *testing.T, dir, envName string) map[string]string {
	t.Helper()
	overlay, err := Overlay(dir, envName)
	if err != nil {
		t.Fatalf("Overlay(%q, %q): %v", dir, envName, err)
	}
	return overlay
}

// Without a Snapshot (shellEnv nil), the overlay must not override a real
// process-env variable: the process env wins and the overlay only fills
// names it doesn't define. Guards against a Connect that runs before
// Snapshot silently letting .env.<env> shadow a real environment value.
func TestResolveVar_NoSnapshotProcessEnvWins(t *testing.T) {
	resetSnapshot(t)
	shellEnv = nil
	clearEnv(t, "GAFFER_LAYER")
	t.Setenv("GAFFER_LAYER", "real")

	got, err := Expand("${GAFFER_LAYER}", map[string]string{"GAFFER_LAYER": "overlay"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "real" {
		t.Fatalf("got %q, want real (process env wins over overlay without a snapshot)", got)
	}
}

// Without a Snapshot, the overlay still supplies a value the process env
// lacks - it fills gaps, it just never overrides.
func TestResolveVar_NoSnapshotOverlayFillsGaps(t *testing.T) {
	resetSnapshot(t)
	shellEnv = nil
	clearEnv(t, "GAFFER_LAYER")

	got, err := Expand("${GAFFER_LAYER}", map[string]string{"GAFFER_LAYER": "overlay"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "overlay" {
		t.Fatalf("got %q, want overlay (fills a gap the process env lacks)", got)
	}
}

func TestExpand_DedupesMissing(t *testing.T) {
	clearEnv(t, "GAFFER_TEST_MISSING")
	_, err := Expand("${GAFFER_TEST_MISSING}/${GAFFER_TEST_MISSING}", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	// Variable named once despite two references.
	if c := countOccurrences(err.Error(), "GAFFER_TEST_MISSING"); c != 1 {
		t.Errorf("expected variable named once, got %d", c)
	}
}

func contains(s, sub string) bool { return countOccurrences(s, sub) > 0 }

func countOccurrences(s, sub string) int {
	n := 0
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			n++
		}
	}
	return n
}
