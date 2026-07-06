package remote

import (
	"errors"
	"testing"
	"time"
)

var classifyTime = time.Date(2026, 6, 28, 14, 32, 0, 0, time.UTC)

func classifyVer(number int64, query string, enabled bool, l *Ledger) Version {
	return Version{
		Number:     number,
		Definition: &Definition{Query: query, EngineVersion: 1, Enabled: enabled, Time: classifyTime},
		Ledger:     l,
	}
}

func classifyGafferLedger(op string) *Ledger {
	return &Ledger{Tool: ToolName, Operation: op, ToolVersion: "1.4.0", Actor: "george@kurrent.io", Revision: "9f8e7d6", Time: classifyTime}
}

func TestClassifyVersion(t *testing.T) {
	prev := classifyVer(0, "v0", true, nil)
	disabledPrev := classifyVer(0, "v0", false, nil)
	enabledPrev := classifyVer(0, "v0", true, nil)
	for _, tc := range []struct {
		name     string
		v        Version
		prev     *Version
		wantKind VersionKind
		wantTool string
		wantExt  bool
	}{
		{"gaffer deploy", classifyVer(1, "v1", true, classifyGafferLedger(OpDeploy)), &prev, KindDeploy, "", false},
		{"gaffer rollback", classifyVer(1, "v1", true, classifyGafferLedger(OpRollback)), &prev, KindRollback, "", false},
		{"gaffer reset", classifyVer(1, "v1", true, classifyGafferLedger(OpReset)), &prev, KindReset, "", false},
		{"gaffer recreate", classifyVer(1, "v1", true, classifyGafferLedger(OpRecreate)), &prev, KindRecreate, "", false},
		{"foreign tool", classifyVer(1, "v1", true, &Ledger{Tool: "KurrentDB Embedded UI", Operation: "create", Time: classifyTime}), &prev, KindChangedByTool, "KurrentDB Embedded UI", true},
		{"metadata-less query change", classifyVer(1, "changed", true, nil), &prev, KindEditedExternally, "", true},
		{"metadata-less enable", classifyVer(1, "v0", true, nil), &disabledPrev, KindEnabled, "", false},
		{"metadata-less disable", classifyVer(1, "v0", false, nil), &enabledPrev, KindDisabled, "", false},
		{"absent enabled is disabled (flip from enabled)", Version{Number: 1, Definition: &Definition{Query: "v0", EngineVersion: 1, Time: classifyTime}}, &enabledPrev, KindDisabled, "", false},
		{"config change (reconfigured)", Version{Number: 1, Definition: &Definition{Query: "v0", EngineVersion: 1, Enabled: true, Config: Config{MaxWriteBatchLength: 1000}, Time: classifyTime}}, &prev, KindReconfigured, "", false},
		{"metadata-less no-op", classifyVer(1, "v0", true, nil), &prev, KindRewritten, "", false},
		{"metadata-less first version", classifyVer(0, "v0", true, nil), nil, KindCreated, "", false},
		{"metadata-less oldest in window", classifyVer(5, "v5", true, nil), nil, KindRewritten, "", false},
		{"tombstone", Version{Number: 2, Deleted: true, Definition: &Definition{Query: "v2", Time: classifyTime}, Ledger: classifyGafferLedger(OpDeploy)}, &prev, KindDeleted, "", false},
		{"unreadable metadata", Version{Number: 1, Definition: &Definition{Query: "v1", Time: classifyTime}, MetaErr: errors.New("bad metadata")}, &prev, KindUnreadable, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kind, tool, _, _ := classifyVersion(tc.v, tc.prev)
			if kind != tc.wantKind || tool != tc.wantTool {
				t.Fatalf("got (%q, %q), want (%q, %q)", kind, tool, tc.wantKind, tc.wantTool)
			}
			cv := ClassifiedVersion{Version: tc.v, Kind: kind, Tool: tool}
			if cv.External() != tc.wantExt {
				t.Errorf("External() = %v, want %v", cv.External(), tc.wantExt)
			}
		})
	}
}
