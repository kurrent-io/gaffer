package drift

import (
	"context"
	"net/http"
	"net/http/httptest"
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
		if got := <-StartConfigDriftCheck(context.Background(), &config.Config{}, t.TempDir(), "", "kurrentdb://x:1?tls=false"); got != nil {
			t.Errorf("got %v, want nil without [database_config]", got)
		}
	})

	t.Run("fetches and compares end to end", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`[{"name": "MaxProjectionStateSize", "value": "16777216"}]`))
		}))
		defer srv.Close()

		cfg := &config.Config{DatabaseConfig: &config.DatabaseConfig{MaxStateSize: new(int64(1024))}}
		conn := "kurrentdb://" + strings.TrimPrefix(srv.URL, "http://") + "?tls=false"
		got := <-StartConfigDriftCheck(context.Background(), cfg, t.TempDir(), "", conn)
		if len(got) != 1 || got[0].Knob != "max_state_size" {
			t.Fatalf("got %+v, want the max_state_size divergence", got)
		}
	})

	t.Run("an unreachable target reads as nothing", func(t *testing.T) {
		cfg := &config.Config{DatabaseConfig: &config.DatabaseConfig{MaxStateSize: new(int64(1024))}}
		if got := <-StartConfigDriftCheck(context.Background(), cfg, t.TempDir(), "", "kurrentdb://127.0.0.1:1?tls=false"); got != nil {
			t.Errorf("got %v, want nil on fetch failure", got)
		}
	})
}
