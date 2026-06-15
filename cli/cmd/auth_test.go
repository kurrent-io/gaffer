package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/kurrent-io/gaffer/cli/internal/config"
)

func TestResolveOAuthEnv(t *testing.T) {
	cfg := &config.Config{Env: map[string]config.Env{
		"prod": {
			Connection: "kurrentdb://prod:2113",
			OAuth:      &config.OAuthConfig{Issuer: "https://idp.example.com", ClientID: "c"},
			Default:    true,
		},
		"basic": {Connection: "kurrentdb://basic:2113"},
	}}

	t.Run("returns an env that has oauth", func(t *testing.T) {
		resolved, err := resolveOAuthEnv(cfg, "prod")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resolved.Name != "prod" || resolved.OAuth == nil {
			t.Errorf("unexpected resolved env: %+v", resolved)
		}
	})

	t.Run("errors when the env has no oauth", func(t *testing.T) {
		_, err := resolveOAuthEnv(cfg, "basic")
		if err == nil || !strings.Contains(err.Error(), "oauth") {
			t.Fatalf("expected a no-oauth error, got %v", err)
		}
	})

	t.Run("errors on an unknown env", func(t *testing.T) {
		_, err := resolveOAuthEnv(cfg, "missing")
		if err == nil {
			t.Fatalf("expected an unknown-env error, got nil")
		}
	})
}

func TestBrowserOpener(t *testing.T) {
	var opened bool
	orig := openBrowser
	openBrowser = func(string) error { opened = true; return nil }
	t.Cleanup(func() { openBrowser = orig })

	run := func() *bytes.Buffer {
		opened = false
		cmd := &cobra.Command{}
		buf := &bytes.Buffer{}
		cmd.SetErr(buf)
		if err := browserOpener(cmd)("https://idp.example.com/auth"); err != nil {
			t.Fatalf("browserOpener: %v", err)
		}
		return buf
	}

	t.Run("opens the browser by default", func(t *testing.T) {
		buf := run()
		if !opened {
			t.Error("expected the browser to be opened")
		}
		if !strings.Contains(buf.String(), "https://idp.example.com/auth") {
			t.Errorf("expected the URL to be printed, got %q", buf.String())
		}
	})

	t.Run("GAFFER_NO_OPEN suppresses opening but still prints the URL", func(t *testing.T) {
		t.Setenv("GAFFER_NO_OPEN", "1")
		buf := run()
		if opened {
			t.Error("expected the browser NOT to be opened with GAFFER_NO_OPEN set")
		}
		if !strings.Contains(buf.String(), "https://idp.example.com/auth") {
			t.Errorf("expected the URL to still be printed, got %q", buf.String())
		}
	})
}
