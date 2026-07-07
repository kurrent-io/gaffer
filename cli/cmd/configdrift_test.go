package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/cliout"
	"github.com/kurrent-io/gaffer/cli/internal/drift"
)

func TestWriteConfigDriftWarnings(t *testing.T) {
	var b bytes.Buffer
	writeConfigDriftWarnings(&b, drift.ConfigDriftResult{})
	if b.Len() != 0 {
		t.Errorf("nothing to report should write nothing, got %q", b.String())
	}
	writeConfigDriftWarnings(&b, drift.ConfigDriftResult{Items: []drift.ConfigDrift{{Knob: "max_state_size", Server: 1, Local: 2, Unit: "bytes"}}})
	if out := b.String(); !strings.Contains(out, "⚠ target config drift: max_state_size is 1 bytes on the server, 2 bytes in gaffer.toml") {
		t.Errorf("warning = %q", out)
	}

	// A failed read warns that the check couldn't run - it must not look
	// identical to "in sync" (UI-1820).
	b.Reset()
	writeConfigDriftWarnings(&b, drift.ConfigDriftResult{Err: errors.New("reading node options: options endpoint returned 401 Unauthorized")})
	if out := b.String(); !strings.Contains(out, "⚠ could not check [database_config] drift: reading node options") {
		t.Errorf("failed-read warning = %q", out)
	}
}

func TestRenderStatusJSONCarriesConfigDrift(t *testing.T) {
	var b bytes.Buffer
	items := []drift.ConfigDrift{{Knob: "execution_timeout", Server: 250, Local: 700, Unit: "ms"}}
	if err := renderStatusJSON(&b, nil, drift.ConfigDriftResult{Items: items}); err != nil {
		t.Fatal(err)
	}
	var report cliout.StatusReportJSON
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
	if report.ConfigDriftError != "" {
		t.Errorf("configDriftError should be omitted when the check ran, got %q", report.ConfigDriftError)
	}

	// A failed read lands in configDriftError so machine consumers can tell
	// "couldn't check" from "in sync" (UI-1820).
	b.Reset()
	if err := renderStatusJSON(&b, nil, drift.ConfigDriftResult{Err: errors.New("reading node options: connection refused")}); err != nil {
		t.Fatal(err)
	}
	report = cliout.StatusReportJSON{}
	if err := json.Unmarshal(b.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, b.String())
	}
	if report.ConfigDriftError != "reading node options: connection refused" {
		t.Errorf("configDriftError = %q", report.ConfigDriftError)
	}
	if len(report.ConfigDrift) != 0 {
		t.Errorf("configDrift should be empty on a failed read, got %+v", report.ConfigDrift)
	}
}
