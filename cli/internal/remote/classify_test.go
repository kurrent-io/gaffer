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
	}{
		{"gaffer deploy", classifyVer(1, "v1", true, classifyGafferLedger(OpDeploy)), &prev, KindDeploy, ""},
		{"gaffer rollback", classifyVer(1, "v1", true, classifyGafferLedger(OpRollback)), &prev, KindRollback, ""},
		{"gaffer reset", classifyVer(1, "v1", true, classifyGafferLedger(OpReset)), &prev, KindReset, ""},
		{"gaffer recreate", classifyVer(1, "v1", true, classifyGafferLedger(OpRecreate)), &prev, KindRecreate, ""},
		{"foreign tool", classifyVer(1, "v1", true, &Ledger{Tool: "KurrentDB Embedded UI", Operation: "create", Time: classifyTime}), &prev, KindUpdatedByTool, "KurrentDB Embedded UI"},
		{"metadata-less query change", classifyVer(1, "changed", true, nil), &prev, KindUpdated, ""},
		{"metadata-less enable", classifyVer(1, "v0", true, nil), &disabledPrev, KindEnabled, ""},
		{"metadata-less disable", classifyVer(1, "v0", false, nil), &enabledPrev, KindDisabled, ""},
		{"absent enabled is disabled (flip from enabled)", Version{Number: 1, Definition: &Definition{Query: "v0", EngineVersion: 1, Time: classifyTime}}, &enabledPrev, KindDisabled, ""},
		{"config change (reconfigured)", Version{Number: 1, Definition: &Definition{Query: "v0", EngineVersion: 1, Enabled: true, Config: Config{MaxWriteBatchLength: 1000}, Time: classifyTime}}, &prev, KindReconfigured, ""},
		{"metadata-less no-op", classifyVer(1, "v0", true, nil), &prev, KindRewritten, ""},
		{"metadata-less first version", classifyVer(0, "v0", true, nil), nil, KindCreated, ""},
		{"metadata-less oldest in window", classifyVer(5, "v5", true, nil), nil, KindRewritten, ""},
		{"tombstone", Version{Number: 2, Deleted: true, Definition: &Definition{Query: "v2", Time: classifyTime}, Ledger: classifyGafferLedger(OpDeploy)}, &prev, KindDeleted, ""},
		{"unreadable metadata", Version{Number: 1, Definition: &Definition{Query: "v1", Time: classifyTime}, MetaErr: errors.New("bad metadata")}, &prev, KindUnreadable, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kind, tool, _, _ := classifyVersion(tc.v, tc.prev)
			if kind != tc.wantKind || tool != tc.wantTool {
				t.Fatalf("got (%q, %q), want (%q, %q)", kind, tool, tc.wantKind, tc.wantTool)
			}
		})
	}
}

func classifyForeignLedger(tool string) *Ledger {
	return &Ledger{Tool: tool, Operation: "create", Time: classifyTime}
}

// The out-of-band warning latches on the first gaffer write: only a non-gaffer
// write after gaffer began managing the projection reads as changed-outside-gaffer.
// Versions are newest-first, as Classify receives them.
func TestClassifyOutOfBand(t *testing.T) {
	gaffer := func(n int64, q string) Version { return classifyVer(n, q, true, classifyGafferLedger(OpDeploy)) }
	hand := func(n int64, q string) Version { return classifyVer(n, q, true, nil) }
	foreign := func(n int64, q string) Version { return classifyVer(n, q, true, classifyForeignLedger("admin ui")) }

	for _, tc := range []struct {
		name     string
		versions []Version // newest-first
		// wantOutside is indexed like versions (newest-first).
		wantOutside []bool
		wantKind    []VersionKind
	}{
		{
			name:        "metadata-less edit after a gaffer deploy warns",
			versions:    []Version{hand(2, "b"), gaffer(1, "a"), gaffer(0, "a0")},
			wantOutside: []bool{true, false, false},
			wantKind:    []VersionKind{KindUpdated, KindDeploy, KindDeploy},
		},
		{
			name:        "hand edits before gaffer are neutral",
			versions:    []Version{gaffer(2, "c"), hand(1, "b"), hand(0, "a")},
			wantOutside: []bool{false, false, false},
			wantKind:    []VersionKind{KindDeploy, KindUpdated, KindCreated},
		},
		{
			name:        "metadata-less server (no gaffer ever) never warns",
			versions:    []Version{hand(2, "c"), hand(1, "b"), hand(0, "a")},
			wantOutside: []bool{false, false, false},
			wantKind:    []VersionKind{KindUpdated, KindUpdated, KindCreated},
		},
		{
			name:        "a foreign tool after gaffer warns",
			versions:    []Version{foreign(1, "b"), gaffer(0, "a")},
			wantOutside: []bool{true, false},
			wantKind:    []VersionKind{KindUpdatedByTool, KindDeploy},
		},
		{
			name:        "a foreign tool before gaffer is neutral",
			versions:    []Version{gaffer(1, "b"), foreign(0, "a")},
			wantOutside: []bool{false, false},
			wantKind:    []VersionKind{KindDeploy, KindUpdatedByTool},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(tc.versions)
			for i, cv := range got {
				if cv.Kind != tc.wantKind[i] {
					t.Errorf("version[%d] kind = %q, want %q", i, cv.Kind, tc.wantKind[i])
				}
				if cv.OutOfBand() != tc.wantOutside[i] {
					t.Errorf("version[%d] OutOfBand() = %v, want %v", i, cv.OutOfBand(), tc.wantOutside[i])
				}
			}
		})
	}
}

func TestIsCreateConflict(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"envelope conflict reply", errors.New("rpc error: code = Unknown desc = Envelope callback expected Updated, received Conflict instead"), true},
		{"typed sentinel", ErrAlreadyExists, true},
		{"wrapped sentinel", errors.Join(errors.New("create orders"), ErrAlreadyExists), true},
		{"unrelated failure", errors.New("rpc error: code = Unavailable desc = leader down"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsCreateConflict(tc.err); got != tc.want {
				t.Errorf("IsCreateConflict(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
