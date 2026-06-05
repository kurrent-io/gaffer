package prompt

import "testing"

func TestEnabledShortCircuitsOnYes(t *testing.T) {
	// --yes must skip prompting without consulting the terminal, so a
	// scripted `--yes` on a TTY still takes the non-interactive path.
	if Enabled(true) {
		t.Fatal("Enabled(true) = true, want false: --yes must disable prompting")
	}
}

func TestEnabledFalseWithoutTTY(t *testing.T) {
	// The test process has no terminal on stdin, so prompting is off
	// even without --yes. This is the CI/piped guard.
	if Enabled(false) {
		t.Fatal("Enabled(false) = true under a non-terminal stdin, want false")
	}
}

func TestOpt(t *testing.T) {
	o := Opt("all")
	if o.Label != "all" || o.Value != "all" {
		t.Fatalf("Opt(\"all\") = %+v, want label and value both \"all\"", o)
	}
}
