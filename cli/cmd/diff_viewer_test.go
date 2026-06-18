package cmd

import (
	"errors"
	"slices"
	"testing"
)

func TestResolveDiffCommand(t *testing.T) {
	found := func(names ...string) func(string) (string, error) {
		return func(n string) (string, error) {
			if slices.Contains(names, n) {
				return "/usr/bin/" + n, nil
			}
			return "", errors.New("not found")
		}
	}
	env := func(v string) func(string) string { return func(string) string { return v } }

	t.Run("override wins over git", func(t *testing.T) {
		argv, ok := resolveDiffCommand(env("delta --paging never"), found("git", "diff"))
		if !ok || !slices.Equal(argv, []string{"delta", "--paging", "never"}) {
			t.Fatalf("argv=%v ok=%v", argv, ok)
		}
	})
	t.Run("git preferred when no override", func(t *testing.T) {
		argv, ok := resolveDiffCommand(env(""), found("git", "diff"))
		if !ok || !slices.Equal(argv, []string{"git", "diff", "--no-index"}) {
			t.Fatalf("argv=%v ok=%v", argv, ok)
		}
	})
	t.Run("diff fallback when git absent", func(t *testing.T) {
		argv, ok := resolveDiffCommand(env(""), found("diff"))
		if !ok || !slices.Equal(argv, []string{"diff", "-u"}) {
			t.Fatalf("argv=%v ok=%v", argv, ok)
		}
	})
	t.Run("none available", func(t *testing.T) {
		if _, ok := resolveDiffCommand(env(""), found()); ok {
			t.Fatal("want ok=false when neither git nor diff is present")
		}
	})
	t.Run("blank override falls through to git", func(t *testing.T) {
		argv, ok := resolveDiffCommand(env("   "), found("git"))
		if !ok || argv[0] != "git" {
			t.Fatalf("argv=%v ok=%v", argv, ok)
		}
	})
}
