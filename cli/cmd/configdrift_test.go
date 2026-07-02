package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

func i64(v int64) *int64 { return &v }
func iptr(v int) *int    { return &v }

func TestConfigDriftItems(t *testing.T) {
	node := &remote.NodeProjectionOptions{
		CompilationTimeoutMs: i64(500),
		ExecutionTimeoutMs:   i64(250),
		MaxStateSizeBytes:    i64(16777216),
	}

	t.Run("nothing declared or no node reads as clean", func(t *testing.T) {
		if got := configDriftItems(nil, node); got != nil {
			t.Errorf("nil config = %v, want nothing", got)
		}
		if got := configDriftItems(&config.DatabaseConfig{MaxStateSize: i64(1)}, nil); got != nil {
			t.Errorf("nil node = %v, want nothing", got)
		}
	})

	t.Run("only declared and diverging knobs report", func(t *testing.T) {
		dc := &config.DatabaseConfig{
			CompilationTimeout: iptr(500),     // matches: silent
			MaxStateSize:       i64(33554432), // diverges: reported
			// execution_timeout undeclared: silent even though the server reports it
		}
		got := configDriftItems(dc, node)
		if len(got) != 1 || got[0].Knob != "max_state_size" || got[0].Server != 16777216 || got[0].Local != 33554432 {
			t.Fatalf("items = %+v, want the single max_state_size divergence", got)
		}
		if want := "max_state_size is 16777216 bytes on the server, 33554432 bytes in gaffer.toml"; got[0].text() != want {
			t.Errorf("text = %q, want %q", got[0].text(), want)
		}
	})

	t.Run("a knob the server doesn't report is silent", func(t *testing.T) {
		dc := &config.DatabaseConfig{ExecutionTimeout: iptr(9999)}
		if got := configDriftItems(dc, &remote.NodeProjectionOptions{}); got != nil {
			t.Errorf("items = %v, want nothing when the option is absent", got)
		}
	})

	t.Run("non-positive max_state_size declares the default, not a value", func(t *testing.T) {
		dc := &config.DatabaseConfig{MaxStateSize: i64(0)}
		if got := configDriftItems(dc, node); got != nil {
			t.Errorf("items = %v, want nothing for the use-the-default sentinel", got)
		}
	})

	t.Run("timeout text joins the unit", func(t *testing.T) {
		dc := &config.DatabaseConfig{ExecutionTimeout: iptr(700)}
		got := configDriftItems(dc, node)
		if len(got) != 1 || got[0].text() != "execution_timeout is 250ms on the server, 700ms in gaffer.toml" {
			t.Fatalf("items = %+v", got)
		}
	})
}

func TestStartConfigDriftCheck(t *testing.T) {
	t.Run("no database_config short-circuits", func(t *testing.T) {
		if got := <-startConfigDriftCheck(context.Background(), &config.Config{}, t.TempDir(), "", "kurrentdb://x:1?tls=false"); got != nil {
			t.Errorf("got %v, want nil without [database_config]", got)
		}
	})

	t.Run("fetches and compares end to end", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`[{"name": "MaxProjectionStateSize", "value": "16777216"}]`))
		}))
		defer srv.Close()

		cfg := &config.Config{DatabaseConfig: &config.DatabaseConfig{MaxStateSize: i64(1024)}}
		conn := "kurrentdb://" + strings.TrimPrefix(srv.URL, "http://") + "?tls=false"
		got := <-startConfigDriftCheck(context.Background(), cfg, t.TempDir(), "", conn)
		if len(got) != 1 || got[0].Knob != "max_state_size" {
			t.Fatalf("got %+v, want the max_state_size divergence", got)
		}
	})

	t.Run("an unreachable target reads as nothing", func(t *testing.T) {
		cfg := &config.Config{DatabaseConfig: &config.DatabaseConfig{MaxStateSize: i64(1024)}}
		if got := <-startConfigDriftCheck(context.Background(), cfg, t.TempDir(), "", "kurrentdb://127.0.0.1:1?tls=false"); got != nil {
			t.Errorf("got %v, want nil on fetch failure", got)
		}
	})
}

func TestWriteConfigDriftWarnings(t *testing.T) {
	var b bytes.Buffer
	writeConfigDriftWarnings(&b, nil)
	if b.Len() != 0 {
		t.Errorf("nothing to report should write nothing, got %q", b.String())
	}
	writeConfigDriftWarnings(&b, []configDrift{{Knob: "max_state_size", Server: 1, Local: 2, unit: "bytes"}})
	if out := b.String(); !strings.Contains(out, "⚠ target config drift: max_state_size is 1 bytes on the server, 2 bytes in gaffer.toml") {
		t.Errorf("warning = %q", out)
	}
}

func TestRenderStatusJSONCarriesConfigDrift(t *testing.T) {
	var b bytes.Buffer
	drift := []configDrift{{Knob: "execution_timeout", Server: 250, Local: 700, unit: "ms"}}
	if err := renderStatusJSON(&b, nil, drift); err != nil {
		t.Fatal(err)
	}
	var report statusReportJSON
	if err := json.Unmarshal(b.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, b.String())
	}
	if len(report.ConfigDrift) != 1 || report.ConfigDrift[0].Knob != "execution_timeout" ||
		report.ConfigDrift[0].Server != 250 || report.ConfigDrift[0].Local != 700 {
		t.Errorf("configDrift = %+v", report.ConfigDrift)
	}
	if report.Projections == nil || len(report.Projections) != 0 {
		t.Errorf("projections should be an empty array, got %v", report.Projections)
	}
}
