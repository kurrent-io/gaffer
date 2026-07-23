package cliout

import (
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

// A metadata-less content change (KindUpdated) carries a changeSummary; a gaffer
// deploy and a foreign tool's write don't (no computed diff), so the field is
// omitted for them.
func TestBuildHistoryJSONChangeSummary(t *testing.T) {
	versions := []remote.ClassifiedVersion{
		{Kind: remote.KindUpdated, HasChange: true, Change: deploy.Comparison{QueryDiffers: true, EmitDiffers: true}},
		{Kind: remote.KindUpdatedByTool, Tool: "Admin UI"},
		{Kind: remote.KindDeploy},
	}
	out := BuildHistoryJSON(versions)
	if out[0].ChangeSummary != "query and emit changed" {
		t.Errorf("updated changeSummary = %q, want %q", out[0].ChangeSummary, "query and emit changed")
	}
	if out[1].ChangeSummary != "" {
		t.Errorf("updated-by changeSummary = %q, want empty", out[1].ChangeSummary)
	}
	if out[2].ChangeSummary != "" {
		t.Errorf("deploy changeSummary = %q, want empty", out[2].ChangeSummary)
	}
}
