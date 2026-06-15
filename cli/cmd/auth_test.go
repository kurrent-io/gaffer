package cmd

import (
	"strings"
	"testing"

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
