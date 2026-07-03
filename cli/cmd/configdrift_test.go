package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/drift"
)

func TestWriteConfigDriftWarnings(t *testing.T) {
	var b bytes.Buffer
	writeConfigDriftWarnings(&b, nil)
	if b.Len() != 0 {
		t.Errorf("nothing to report should write nothing, got %q", b.String())
	}
	writeConfigDriftWarnings(&b, []drift.ConfigDrift{{Knob: "max_state_size", Server: 1, Local: 2, Unit: "bytes"}})
	if out := b.String(); !strings.Contains(out, "⚠ target config drift: max_state_size is 1 bytes on the server, 2 bytes in gaffer.toml") {
		t.Errorf("warning = %q", out)
	}
}

func TestRenderStatusJSONCarriesConfigDrift(t *testing.T) {
	var b bytes.Buffer
	items := []drift.ConfigDrift{{Knob: "execution_timeout", Server: 250, Local: 700, Unit: "ms"}}
	if err := renderStatusJSON(&b, nil, items); err != nil {
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
