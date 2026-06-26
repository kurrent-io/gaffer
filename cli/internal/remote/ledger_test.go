package remote

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var ledgerTime = time.Date(2026, 6, 26, 10, 0, 0, 0, time.UTC)

// ledgerEvent is a $ProjectionUpdated carrying the given metadata JSON, the shape
// the server synthesises on read (caller keys at the top level).
func ledgerEvent(metadata string) *kurrentdb.ResolvedEvent {
	return &kurrentdb.ResolvedEvent{Event: &kurrentdb.RecordedEvent{
		EventType:    projectionUpdatedType,
		UserMetadata: []byte(metadata),
		CreatedDate:  ledgerTime,
	}}
}

const (
	// A gaffer-written entry plus the server's synthetic $ keys it round-trips with.
	gafferMetadata = `{"tool":"Gaffer","tool_version":"1.2.3","operation":"deploy","revision":"abc123","actor":"admin","$schema.name":"$ProjectionUpdated","$schema.format":"Json"}`
	// A metadata-less lifecycle write: only the server's synthetic keys.
	syntheticMetadata = `{"$schema.name":"$ProjectionUpdated","$schema.format":"Json"}`
)

func TestParseLedger(t *testing.T) {
	l, ok := parseLedger([]byte(gafferMetadata), ledgerTime)
	if !ok {
		t.Fatal("parseLedger: ok=false for a tool entry")
	}
	if l.Tool != "Gaffer" || l.ToolVersion != "1.2.3" || l.Operation != "deploy" {
		t.Errorf("core fields = %+v", l)
	}
	if l.Revision != "abc123" || l.Actor != "admin" {
		t.Errorf("optional fields = revision %q actor %q", l.Revision, l.Actor)
	}
	if !l.Time.Equal(ledgerTime) {
		t.Errorf("Time = %v, want the event CreatedDate %v", l.Time, ledgerTime)
	}
}

func TestParseLedgerOmittedOptionalFields(t *testing.T) {
	// A foreign tool that writes only the required keys (no revision/actor).
	l, ok := parseLedger([]byte(`{"tool":"KurrentDB Admin UI","tool_version":"26.2.0","operation":"create"}`), ledgerTime)
	if !ok {
		t.Fatal("parseLedger: ok=false")
	}
	if l.Tool != "KurrentDB Admin UI" {
		t.Errorf("Tool = %q", l.Tool)
	}
	if l.Revision != "" || l.Actor != "" {
		t.Errorf("absent optional fields should be empty: revision %q actor %q", l.Revision, l.Actor)
	}
}

func TestParseLedgerRejectsNonTool(t *testing.T) {
	for name, md := range map[string]string{
		"synthetic-only": syntheticMetadata,
		"empty":          "",
		"bad json":       `{not json`,
		"empty object":   `{}`,
	} {
		if _, ok := parseLedger([]byte(md), ledgerTime); ok {
			t.Errorf("%s: parseLedger ok=true, want rejected", name)
		}
	}
}

func TestReadLedgerSkipsLifecycleToNewestToolEntry(t *testing.T) {
	// Newest-first, with the buried gaffer entry under a metadata-less lifecycle
	// write, a resolved link with no event, and a non-$ProjectionUpdated event -
	// all of which must be skipped.
	next := recvSeq(
		recvStep{ev: ledgerEvent(syntheticMetadata)},
		recvStep{ev: &kurrentdb.ResolvedEvent{Event: nil}},                                              // resolved link, no event
		recvStep{ev: &kurrentdb.ResolvedEvent{Event: &kurrentdb.RecordedEvent{EventType: "$metadata"}}}, // other type
		recvStep{ev: ledgerEvent(gafferMetadata)},
	)
	l, err := readLedger(next)
	if err != nil {
		t.Fatalf("readLedger: %v", err)
	}
	if l.Tool != "Gaffer" || l.Operation != "deploy" {
		t.Errorf("got %+v, want the buried gaffer entry", l)
	}
}

func TestReadLedgerClassifiesReadError(t *testing.T) {
	// A failure partway through the scan must surface the typed sentinel, not be
	// mistaken for ErrNoLedger (which would tell a caller "degrade" when the truth
	// is "couldn't read").
	next := recvSeq(recvStep{err: status.New(codes.Unavailable, "boom").Err()})
	_, err := readLedger(next)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
	if errors.Is(err, ErrNoLedger) {
		t.Fatal("a read error must not classify as ErrNoLedger")
	}
}

func TestReadLedgerAbsentStreamIsNotFound(t *testing.T) {
	// On a backward read a missing stream surfaces as ResourceNotFound at the first
	// Recv (not io.EOF). ReadLedger keeps that distinct from ErrNoLedger so a caller
	// can tell "projection gone" from "exists but untracked".
	next := recvSeq(recvStep{err: status.New(codes.NotFound, "stream not found").Err()})
	_, err := readLedger(next)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if errors.Is(err, ErrNoLedger) {
		t.Fatal("an absent stream must not classify as ErrNoLedger")
	}
}

func TestReadLedgerNoToolEntryIsErrNoLedger(t *testing.T) {
	next := recvSeq(
		recvStep{ev: ledgerEvent(syntheticMetadata)},
		recvStep{ev: ledgerEvent(syntheticMetadata)},
	)
	if _, err := readLedger(next); !errors.Is(err, ErrNoLedger) {
		t.Fatalf("err = %v, want ErrNoLedger", err)
	}
}

func TestReadLedgerEmptyStreamIsErrNoLedger(t *testing.T) {
	if _, err := readLedger(recvSeq()); !errors.Is(err, ErrNoLedger) {
		t.Fatalf("err = %v, want ErrNoLedger", err)
	}
}

func TestLedgerMetadataNil(t *testing.T) {
	// nil ledger writes no caller properties (the metadata-less / old-server path);
	// a non-nil ledger bridges to its metadata() unchanged.
	if got := ledgerMetadata(nil); got != nil {
		t.Errorf("ledgerMetadata(nil) = %v, want nil", got)
	}
	l := Ledger{Tool: ToolName, ToolVersion: "1.0", Operation: OpDeploy}
	if got := ledgerMetadata(&l); !reflect.DeepEqual(got, l.metadata()) {
		t.Errorf("ledgerMetadata(&l) = %v, want %v", got, l.metadata())
	}
}

func TestLedgerMetadataDropsEmptyFields(t *testing.T) {
	// Best-effort fields absent from the struct must be absent from the wire, not
	// emitted as empty strings (a "revision":"" would read back as a present-but-
	// blank field and mislead a UI).
	m := Ledger{Tool: ToolName, ToolVersion: "1.0", Operation: OpDeploy}.metadata()
	if _, ok := m[ledgerKeyRevision]; ok {
		t.Error("empty Revision must be omitted from metadata")
	}
	if _, ok := m[ledgerKeyActor]; ok {
		t.Error("empty Actor must be omitted from metadata")
	}
	if len(m) != 3 {
		t.Errorf("metadata = %v, want exactly tool/tool_version/operation", m)
	}
}

func TestLedgerMetadataRoundTrip(t *testing.T) {
	// What gaffer writes parses back to the same fields (Time aside). NB this is a
	// Go-JSON symmetry check, not the wire path: the real round-trip is
	// map -> structpb -> server property-metadata -> JSON synthesis (server adds
	// $schema.*) -> UserMetadata. structpb type coercion, top-level-key placement,
	// and $-key collision are only provable by the live integration test against a
	// metadata-capable server (commit 5), not here.
	in := Ledger{Tool: ToolName, ToolVersion: "9.9.9", Operation: OpDeploy, Revision: "deadbeef+changes", Actor: "ci"}
	raw, err := json.Marshal(in.metadata())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out, ok := parseLedger(raw, ledgerTime)
	if !ok {
		t.Fatal("parseLedger: ok=false")
	}
	if out.Tool != in.Tool || out.ToolVersion != in.ToolVersion || out.Operation != in.Operation || out.Revision != in.Revision || out.Actor != in.Actor {
		t.Errorf("round-trip mismatch:\n in  %+v\n out %+v", in, out)
	}
}
