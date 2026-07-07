package drift

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

func TestConfigDriftItems(t *testing.T) {
	node := &remote.NodeProjectionOptions{
		CompilationTimeoutMs: new(int64(500)),
		ExecutionTimeoutMs:   new(int64(250)),
		MaxStateSizeBytes:    new(int64(16777216)),
	}

	t.Run("nothing declared or no node reads as clean", func(t *testing.T) {
		if got := ConfigDriftItems(nil, node); got != nil {
			t.Errorf("nil config = %v, want nothing", got)
		}
		if got := ConfigDriftItems(&config.DatabaseConfig{MaxStateSize: new(int64(1))}, nil); got != nil {
			t.Errorf("nil node = %v, want nothing", got)
		}
	})

	t.Run("only declared and diverging knobs report", func(t *testing.T) {
		dc := &config.DatabaseConfig{
			CompilationTimeout: new(500),             // matches: silent
			MaxStateSize:       new(int64(33554432)), // diverges: reported
			// execution_timeout undeclared: silent even though the server reports it
		}
		got := ConfigDriftItems(dc, node)
		if len(got) != 1 || got[0].Knob != "max_state_size" || got[0].Server != 16777216 || got[0].Local != 33554432 {
			t.Fatalf("items = %+v, want the single max_state_size divergence", got)
		}
		if want := "max_state_size is 16777216 bytes on the server, 33554432 bytes in gaffer.toml"; got[0].Text() != want {
			t.Errorf("text = %q, want %q", got[0].Text(), want)
		}
	})

	t.Run("a knob the server doesn't report is silent", func(t *testing.T) {
		dc := &config.DatabaseConfig{ExecutionTimeout: new(9999)}
		if got := ConfigDriftItems(dc, &remote.NodeProjectionOptions{}); got != nil {
			t.Errorf("items = %v, want nothing when the option is absent", got)
		}
	})

	t.Run("non-positive max_state_size declares the default, not a value", func(t *testing.T) {
		dc := &config.DatabaseConfig{MaxStateSize: new(int64(0))}
		if got := ConfigDriftItems(dc, node); got != nil {
			t.Errorf("items = %v, want nothing for the use-the-default sentinel", got)
		}
	})

	t.Run("timeout text joins the unit", func(t *testing.T) {
		dc := &config.DatabaseConfig{ExecutionTimeout: new(700)}
		got := ConfigDriftItems(dc, node)
		if len(got) != 1 || got[0].Text() != "execution_timeout is 250ms on the server, 700ms in gaffer.toml" {
			t.Fatalf("items = %+v", got)
		}
	})
}

func TestStartConfigDriftCheck(t *testing.T) {
	t.Run("no database_config short-circuits", func(t *testing.T) {
		got := <-StartConfigDriftCheck(context.Background(), &config.Config{}, t.TempDir(), config.ResolvedEnv{Connection: "kurrentdb://x:1?tls=false"})
		if got.Items != nil || got.Err != nil {
			t.Errorf("got %+v, want the zero result without [database_config]", got)
		}
	})

	t.Run("fetches and compares end to end", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`[{"name": "MaxProjectionStateSize", "value": "16777216"}]`))
		}))
		defer srv.Close()

		cfg := &config.Config{DatabaseConfig: &config.DatabaseConfig{MaxStateSize: new(int64(1024))}}
		conn := "kurrentdb://" + strings.TrimPrefix(srv.URL, "http://") + "?tls=false"
		got := <-StartConfigDriftCheck(context.Background(), cfg, t.TempDir(), config.ResolvedEnv{Connection: conn})
		if got.Err != nil || len(got.Items) != 1 || got.Items[0].Knob != "max_state_size" {
			t.Fatalf("got %+v, want the max_state_size divergence", got)
		}
	})

	t.Run("env-file credentials reach the node read", func(t *testing.T) {
		// The UI-1820 repro: credentials only in .env.<env>, none in the
		// connection string. The read must authenticate with them - before
		// the fix it went out anonymous and the 401 read as "no drift".
		var gotAuth string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			if gotAuth == "" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`[{"name": "MaxProjectionStateSize", "value": "16777216"}]`))
		}))
		defer srv.Close()

		// Shell env outranks the overlay (envvar precedence), so a stray
		// login in the test environment would mask the .env file this case
		// is about. t.Setenv registers restoration; the unset makes the
		// variables absent, not empty.
		for _, v := range []string{"KURRENTDB_USERNAME", "KURRENTDB_PASSWORD"} {
			t.Setenv(v, "")
			_ = os.Unsetenv(v)
		}

		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, ".env.production"), []byte("KURRENTDB_USERNAME=envuser\nKURRENTDB_PASSWORD=envpass\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg := &config.Config{DatabaseConfig: &config.DatabaseConfig{MaxStateSize: new(int64(1024))}}
		conn := "kurrentdb://" + strings.TrimPrefix(srv.URL, "http://") + "?tls=false"
		got := <-StartConfigDriftCheck(context.Background(), cfg, root, config.ResolvedEnv{Name: "production", Connection: conn})
		if got.Err != nil || len(got.Items) != 1 {
			t.Fatalf("got %+v, want the divergence detected through .env credentials", got)
		}
		want := "Basic " + base64.StdEncoding.EncodeToString([]byte("envuser:envpass"))
		if gotAuth != want {
			t.Errorf("Authorization = %q, want the .env credentials %q", gotAuth, want)
		}
	})

	t.Run("an auth refusal surfaces, not silence", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		}))
		defer srv.Close()

		cfg := &config.Config{DatabaseConfig: &config.DatabaseConfig{MaxStateSize: new(int64(1024))}}
		conn := "kurrentdb://" + strings.TrimPrefix(srv.URL, "http://") + "?tls=false"
		got := <-StartConfigDriftCheck(context.Background(), cfg, t.TempDir(), config.ResolvedEnv{Connection: conn})
		if got.Err == nil || !strings.Contains(got.Err.Error(), "401") {
			t.Fatalf("got %+v, want the 401 surfaced", got)
		}
		if got.Items != nil {
			t.Errorf("a failed read must not report items, got %+v", got.Items)
		}
	})

	t.Run("an unreachable target surfaces, not silence", func(t *testing.T) {
		cfg := &config.Config{DatabaseConfig: &config.DatabaseConfig{MaxStateSize: new(int64(1024))}}
		got := <-StartConfigDriftCheck(context.Background(), cfg, t.TempDir(), config.ResolvedEnv{Connection: "kurrentdb://127.0.0.1:1?tls=false"})
		if got.Err == nil || !strings.Contains(got.Err.Error(), "reading node options") {
			t.Errorf("got %+v, want the fetch failure surfaced", got)
		}
	})

	t.Run("OAuth and cert envs report unsupported without a request", func(t *testing.T) {
		// The node-options read speaks basic auth only; an attempt on these
		// envs could only fail. The check says it didn't run - a permanent
		// per-command warning about an unfixable 401 would be worse.
		cfg := &config.Config{DatabaseConfig: &config.DatabaseConfig{MaxStateSize: new(int64(1024))}}
		for name, env := range map[string]config.ResolvedEnv{
			"oauth": {Connection: "kurrentdb://x:1", OAuth: &config.OAuthConfig{}},
			"cert":  {Connection: "kurrentdb://x:1", Cert: &config.CertAuth{}},
		} {
			got := <-StartConfigDriftCheck(context.Background(), cfg, t.TempDir(), env)
			if got.Err == nil || !strings.Contains(got.Err.Error(), "not supported") {
				t.Errorf("%s: got %+v, want the unsupported-auth reason", name, got)
			}
		}
	})
}
