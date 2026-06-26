package lsp

import (
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/testutil"
)

func TestLiveDebugTarget(t *testing.T) {
	cases := []struct {
		name string
		envs []config.EnvDescription
		want string
	}{
		{"none", nil, ""},
		{"one default", []config.EnvDescription{{Name: "local", Default: true}}, "local"},
		{"one non-default", []config.EnvDescription{{Name: "cloud"}}, "cloud"},
		{"many with a default", []config.EnvDescription{{Name: "cloud"}, {Name: "local", Default: true}}, "local"},
		{"many no default", []config.EnvDescription{{Name: "cloud"}, {Name: "local"}}, ""},
		// Invalid config (strict load rejects >1 default); the loose
		// describe path still emits it, so the target must be ambiguous.
		{"multiple defaults", []config.EnvDescription{{Name: "cloud", Default: true}, {Name: "local", Default: true}}, ""},
		{"sole env also marked default", []config.EnvDescription{{Name: "cloud", Default: true}}, "cloud"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := liveDebugTarget(tc.envs); got != tc.want {
				t.Errorf("liveDebugTarget(%v) = %q, want %q", tc.envs, got, tc.want)
			}
		})
	}
}

func TestEmitCodeLenses_EnvGating(t *testing.T) {
	const uri = "file:///x/gaffer.toml"
	// Zero-range fixtures collapse onto the projection header, so no
	// per-fixture Debug lens is emitted - keeps these assertions about
	// the projection-level (live) Debug lens and the dropdown only.
	withFixture := []config.FixtureDescription{{Name: "happy"}}

	find := func(lenses []CodeLens, intent string) (CodeLens, bool) {
		for _, l := range lenses {
			if l.Data != nil && l.Data.Intent == intent {
				return l, true
			}
		}
		return CodeLens{}, false
	}
	debugEnv := func(l CodeLens) string {
		return testutil.MustType[projectionArgs](t, l.Command.Arguments[0]).Env
	}
	pickArgs := func(l CodeLens) projectionPickArgs {
		return testutil.MustType[projectionPickArgs](t, l.Command.Arguments[0])
	}

	t.Run("no env, no fixture: no lenses", func(t *testing.T) {
		lenses := emitCodeLenses(config.Description{
			Projections: []config.ProjectionDescription{{Name: "p"}},
		}, uri)
		if len(lenses) != 0 {
			t.Fatalf("expected no lenses, got %+v", lenses)
		}
	})

	t.Run("no env, with fixture: dropdown only, no live Debug", func(t *testing.T) {
		lenses := emitCodeLenses(config.Description{
			Projections: []config.ProjectionDescription{{Name: "p", Fixtures: withFixture}},
		}, uri)
		if _, ok := find(lenses, IntentDebug); ok {
			t.Error("did not expect a live Debug lens without an env")
		}
		choose, ok := find(lenses, IntentDebugChoose)
		if !ok {
			t.Fatal("expected a Debug from... lens for the fixture")
		}
		if ca := pickArgs(choose); len(ca.FixtureNames) != 1 || len(ca.Envs) != 0 {
			t.Errorf("dropdown args = %+v, want 1 fixture, 0 envs", ca)
		}
	})

	t.Run("default env: live Debug targets it, dropdown lists all envs", func(t *testing.T) {
		lenses := emitCodeLenses(config.Description{
			Projections:  []config.ProjectionDescription{{Name: "p"}},
			Environments: []config.EnvDescription{{Name: "cloud"}, {Name: "local", Default: true}},
		}, uri)
		dbg, ok := find(lenses, IntentDebug)
		if !ok {
			t.Fatal("expected a live Debug lens")
		}
		if got := debugEnv(dbg); got != "local" {
			t.Errorf("Debug env = %q, want local (the default)", got)
		}
		choose, ok := find(lenses, IntentDebugChoose)
		if !ok || len(pickArgs(choose).Envs) != 2 {
			t.Error("expected a dropdown offering both envs")
		}
	})

	t.Run("sole non-default env: live Debug targets it", func(t *testing.T) {
		lenses := emitCodeLenses(config.Description{
			Projections:  []config.ProjectionDescription{{Name: "p"}},
			Environments: []config.EnvDescription{{Name: "cloud"}},
		}, uri)
		dbg, ok := find(lenses, IntentDebug)
		if !ok {
			t.Fatal("expected a live Debug lens for the sole env")
		}
		if got := debugEnv(dbg); got != "cloud" {
			t.Errorf("Debug env = %q, want cloud", got)
		}
	})

	t.Run("many envs, no default: no live Debug, dropdown still offers them", func(t *testing.T) {
		lenses := emitCodeLenses(config.Description{
			Projections:  []config.ProjectionDescription{{Name: "p"}},
			Environments: []config.EnvDescription{{Name: "cloud"}, {Name: "local"}},
		}, uri)
		if _, ok := find(lenses, IntentDebug); ok {
			t.Error("did not expect a live Debug lens with 2 envs and no default")
		}
		choose, ok := find(lenses, IntentDebugChoose)
		if !ok || len(pickArgs(choose).Envs) != 2 {
			t.Error("expected a Debug from... lens offering both envs")
		}
	})
}
