package remote

import (
	"errors"
	"testing"
	"time"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// versionEvent builds a $ProjectionUpdated at a given stream revision, carrying
// the persisted-state data and (optional) tool metadata.
func versionEvent(number uint64, data, metadata string) *kurrentdb.ResolvedEvent {
	return &kurrentdb.ResolvedEvent{Event: &kurrentdb.RecordedEvent{
		EventType:    projectionUpdatedType,
		EventNumber:  number,
		Data:         []byte(data),
		UserMetadata: []byte(metadata),
		CreatedDate:  ledgerTime,
	}}
}

func TestReadHistoryNewestFirstWithTotal(t *testing.T) {
	// Head read: newest-first, and the head event's revision + 1 is the total.
	next := recvSeq(
		recvStep{ev: versionEvent(7, `{"query":"v7"}`, gafferMetadata)},
		recvStep{ev: versionEvent(6, `{"query":"v6"}`, syntheticMetadata)},
		recvStep{ev: versionEvent(5, `{"query":"v5"}`, "")},
	)
	versions, total, err := readHistory(next, "orders", true, HistoryHardCap)
	if err != nil {
		t.Fatalf("readHistory: %v", err)
	}
	if total != 8 {
		t.Fatalf("total = %d, want 8 (head revision 7 + 1)", total)
	}
	if len(versions) != 3 {
		t.Fatalf("got %d versions, want 3", len(versions))
	}
	if versions[0].Number != 7 || versions[0].Definition.Query != "v7" {
		t.Errorf("versions[0] = %+v, want the newest (v7)", versions[0])
	}
	if versions[0].Ledger == nil || versions[0].Ledger.Tool != "Gaffer" {
		t.Errorf("versions[0] ledger = %+v, want a Gaffer entry", versions[0].Ledger)
	}
	if versions[1].Ledger != nil {
		t.Errorf("versions[1] ledger = %+v, want nil for a metadata-less write", versions[1].Ledger)
	}
}

func TestReadHistoryPagedReadHasNoTotal(t *testing.T) {
	// A paged read (not from the head) can't see the head, so total is -1.
	next := recvSeq(recvStep{ev: versionEvent(3, `{"query":"v3"}`, "")})
	_, total, err := readHistory(next, "orders", false, HistoryHardCap)
	if err != nil {
		t.Fatalf("readHistory: %v", err)
	}
	if total != -1 {
		t.Fatalf("total = %d, want -1 on a paged read", total)
	}
}

func TestReadHistoryKeepsTombstoneAsDeletedVersion(t *testing.T) {
	// A delete (e.g. the first half of a recreate) is an audit event, kept as a
	// Deleted version rather than read past or surfaced as ErrNotFound.
	next := recvSeq(
		recvStep{ev: versionEvent(2, `{"query":"v2","deleted":true}`, gafferMetadata)},
		recvStep{ev: versionEvent(1, `{"query":"v1"}`, gafferMetadata)},
	)
	versions, _, err := readHistory(next, "orders", true, HistoryHardCap)
	if err != nil {
		t.Fatalf("readHistory: %v", err)
	}
	if !versions[0].Deleted {
		t.Errorf("versions[0].Deleted = false, want true for a tombstone")
	}
	if versions[1].Deleted {
		t.Errorf("versions[1].Deleted = true, want false for a live version")
	}
}

func TestReadHistoryRespectsLimit(t *testing.T) {
	next := recvSeq(
		recvStep{ev: versionEvent(7, `{"query":"v7"}`, "")},
		recvStep{ev: versionEvent(6, `{"query":"v6"}`, "")},
		recvStep{ev: versionEvent(5, `{"query":"v5"}`, "")},
	)
	versions, _, err := readHistory(next, "orders", true, 2)
	if err != nil {
		t.Fatalf("readHistory: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("got %d versions, want 2 (limit)", len(versions))
	}
	if versions[0].Number != 7 || versions[1].Number != 6 {
		t.Errorf("got %d,%d want the two newest 7,6", versions[0].Number, versions[1].Number)
	}
}

func TestReadHistorySkipsNonStateEvents(t *testing.T) {
	other := &kurrentdb.ResolvedEvent{Event: &kurrentdb.RecordedEvent{EventType: "$metadata"}}
	next := recvSeq(
		recvStep{ev: other},
		recvStep{ev: nil}, // degenerate (nil, nil)
		recvStep{ev: &kurrentdb.ResolvedEvent{Event: nil}}, // link with no event
		recvStep{ev: versionEvent(4, `{"query":"v4"}`, "")},
	)
	versions, _, err := readHistory(next, "orders", true, HistoryHardCap)
	if err != nil {
		t.Fatalf("readHistory: %v", err)
	}
	if len(versions) != 1 || versions[0].Number != 4 {
		t.Fatalf("got %+v, want only the one state event", versions)
	}
}

func TestReadHistoryMalformedMetadataFlagsVersionNotAbort(t *testing.T) {
	// A corrupt metadata blob on one version marks that version (MetaErr) and the
	// read continues, so a single bad entry doesn't blank the whole audit log.
	next := recvSeq(
		recvStep{ev: versionEvent(1, `{"query":"v1"}`, "{not json")},
		recvStep{ev: versionEvent(0, `{"query":"v0"}`, gafferMetadata)},
	)
	versions, _, err := readHistory(next, "orders", true, HistoryHardCap)
	if err != nil {
		t.Fatalf("readHistory aborted on a malformed entry: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("got %d versions, want 2 (bad entry kept)", len(versions))
	}
	if !errors.Is(versions[0].MetaErr, ErrMalformedLedger) || versions[0].Ledger != nil {
		t.Errorf("v1 = %+v, want MetaErr set and no Ledger", versions[0])
	}
	if versions[1].MetaErr != nil || versions[1].Ledger == nil {
		t.Errorf("v0 = %+v, want a clean ledger entry", versions[1])
	}
}

func TestReadHistoryMalformedStateAborts(t *testing.T) {
	// A persisted-state that won't decode is fatal: there's no definition to place.
	next := recvSeq(recvStep{ev: versionEvent(1, "{not json", "")})
	if _, _, err := readHistory(next, "orders", true, HistoryHardCap); err == nil {
		t.Fatal("want a decode error on malformed persisted state, got nil")
	}
}

func TestReadHistoryClassifiesReadError(t *testing.T) {
	next := recvSeq(recvStep{err: status.New(codes.NotFound, "stream not found").Err()})
	if _, _, err := readHistory(next, "orders", true, HistoryHardCap); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestReadHistoryCapturesEventTime(t *testing.T) {
	when := time.Date(2026, 6, 28, 14, 32, 0, 0, time.UTC)
	ev := &kurrentdb.ResolvedEvent{Event: &kurrentdb.RecordedEvent{EventType: projectionUpdatedType, EventNumber: 0, Data: []byte(`{"query":"q"}`), CreatedDate: when}}
	versions, _, err := readHistory(recvSeq(recvStep{ev: ev}), "orders", true, HistoryHardCap)
	if err != nil {
		t.Fatalf("readHistory: %v", err)
	}
	if !versions[0].Definition.Time.Equal(when) {
		t.Fatalf("Time = %v, want the event CreatedDate %v", versions[0].Definition.Time, when)
	}
}
