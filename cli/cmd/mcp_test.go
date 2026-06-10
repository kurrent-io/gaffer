package cmd

import "testing"

func TestPeekProjectOverride(t *testing.T) {
	t.Run("flag with space", func(t *testing.T) {
		t.Setenv(EnvProject, "")
		if got := PeekProjectOverride([]string{"mcp", "--project", "/foo"}); got != "/foo" {
			t.Fatalf("got %q, want /foo", got)
		}
	})
	t.Run("flag with equals", func(t *testing.T) {
		t.Setenv(EnvProject, "")
		if got := PeekProjectOverride([]string{"mcp", "--project=/bar"}); got != "/bar" {
			t.Fatalf("got %q, want /bar", got)
		}
	})
	t.Run("env when no flag", func(t *testing.T) {
		t.Setenv(EnvProject, "/baz")
		if got := PeekProjectOverride([]string{"mcp"}); got != "/baz" {
			t.Fatalf("got %q, want /baz", got)
		}
	})
	t.Run("flag wins over env", func(t *testing.T) {
		t.Setenv(EnvProject, "/env")
		if got := PeekProjectOverride([]string{"mcp", "--project", "/flag"}); got != "/flag" {
			t.Fatalf("got %q, want /flag", got)
		}
	})
	t.Run("neither set", func(t *testing.T) {
		t.Setenv(EnvProject, "")
		if got := PeekProjectOverride([]string{"dev", "foo"}); got != "" {
			t.Fatalf("got %q, want empty", got)
		}
	})
	t.Run("trailing --project without value falls through to env", func(t *testing.T) {
		t.Setenv(EnvProject, "/env")
		if got := PeekProjectOverride([]string{"mcp", "--project"}); got != "/env" {
			t.Fatalf("got %q, want /env", got)
		}
	})
}
