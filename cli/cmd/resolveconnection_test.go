package cmd

import (
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/config"
)

func envConfig() *config.Config {
	return &config.Config{
		Env: map[string]config.Env{
			"local": {Connection: "esdb://local:2113", Default: true},
			"prod":  {Connection: "esdb://prod:2113"},
		},
	}
}

func TestResolveConnection(t *testing.T) {
	t.Run("--connection override has no env name", func(t *testing.T) {
		got, err := resolveConnection(&devOpts{Connection: "esdb://adhoc"}, envConfig())
		if err != nil {
			t.Fatal(err)
		}
		if got.Connection != "esdb://adhoc" || got.Name != "" {
			t.Fatalf("got %+v, want {Connection: esdb://adhoc, Name: \"\"}", got)
		}
	})

	t.Run("--env selects the named env", func(t *testing.T) {
		got, err := resolveConnection(&devOpts{Env: "prod"}, envConfig())
		if err != nil {
			t.Fatal(err)
		}
		if got.Name != "prod" || got.Connection != "esdb://prod:2113" {
			t.Fatalf("got %+v, want prod env", got)
		}
	})

	t.Run("unknown --env errors", func(t *testing.T) {
		if _, err := resolveConnection(&devOpts{Env: "staging"}, envConfig()); err == nil {
			t.Fatal("expected error for unknown env")
		}
	})

	t.Run("no flag uses the default env", func(t *testing.T) {
		got, err := resolveConnection(&devOpts{}, envConfig())
		if err != nil {
			t.Fatal(err)
		}
		if got.Name != "local" || got.Connection != "esdb://local:2113" {
			t.Fatalf("got %+v, want default (local) env", got)
		}
	})

	t.Run("no default and no flag yields empty, no error", func(t *testing.T) {
		cfg := &config.Config{Env: map[string]config.Env{"prod": {Connection: "esdb://prod:2113"}}}
		got, err := resolveConnection(&devOpts{}, cfg)
		if err != nil {
			t.Fatalf("expected no error (fixtures fallback), got %v", err)
		}
		if got.Connection != "" || got.Name != "" {
			t.Fatalf("got %+v, want empty", got)
		}
	})
}
