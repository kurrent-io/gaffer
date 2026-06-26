package remote

import (
	"errors"
	"testing"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
)

func serverInfoEvent(data string) *kurrentdb.ResolvedEvent {
	return &kurrentdb.ResolvedEvent{Event: &kurrentdb.RecordedEvent{EventType: serverInfoType, Data: []byte(data)}}
}

func TestReadServerInfoProduction(t *testing.T) {
	for _, tc := range []struct {
		name     string
		data     string
		wantName string
		wantProd *bool
		wantIs   bool
	}{
		{"production true", `{"name":"orders-prod","production":true}`, "orders-prod", new(true), true},
		{"production false", `{"name":"staging","production":false}`, "staging", new(false), false},
		{"production unset", `{"name":"dev"}`, "dev", nil, false},
		{"name unset", `{"production":true}`, "", new(true), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := readServerInfo(recvSeq(recvStep{ev: serverInfoEvent(tc.data)}))
			if err != nil {
				t.Fatalf("readServerInfo: %v", err)
			}
			if got == nil {
				t.Fatal("got nil, want server info")
			}
			if got.Name != tc.wantName {
				t.Errorf("Name = %q, want %q", got.Name, tc.wantName)
			}
			switch {
			case tc.wantProd == nil && got.Production != nil:
				t.Errorf("Production = %v, want nil", *got.Production)
			case tc.wantProd != nil && (got.Production == nil || *got.Production != *tc.wantProd):
				t.Errorf("Production = %v, want %v", got.Production, *tc.wantProd)
			}
			if got.IsProduction() != tc.wantIs {
				t.Errorf("IsProduction() = %v, want %v", got.IsProduction(), tc.wantIs)
			}
		})
	}
}

func TestReadServerInfoAbsent(t *testing.T) {
	// No $ServerInfo event in the stream (exhausted) -> no info, no error.
	got, err := readServerInfo(recvSeq())
	if err != nil || got != nil {
		t.Fatalf("got (%+v, %v), want (nil, nil) for an empty stream", got, err)
	}
}

func TestReadServerInfoSkipsNonMatchingEvents(t *testing.T) {
	other := &kurrentdb.ResolvedEvent{Event: &kurrentdb.RecordedEvent{EventType: "$metadata"}}
	linkOnly := &kurrentdb.ResolvedEvent{Event: nil}
	next := recvSeq(
		recvStep{ev: other},
		recvStep{ev: nil}, // degenerate (nil, nil): skip, not panic
		recvStep{ev: linkOnly},
		recvStep{ev: serverInfoEvent(`{"name":"c","production":true}`)},
	)
	got, err := readServerInfo(next)
	if err != nil {
		t.Fatalf("readServerInfo: %v", err)
	}
	if got == nil || !got.IsProduction() {
		t.Fatalf("got %+v, want production cluster c", got)
	}
}

func TestReadServerInfoReadError(t *testing.T) {
	// A mid-stream non-EOF, non-NotFound error propagates (not swallowed as "no
	// info") so a genuine read failure surfaces to the caller.
	_, err := readServerInfo(recvSeq(recvStep{err: errors.New("boom")}))
	if err == nil {
		t.Fatal("a read error should propagate, not be treated as absent")
	}
}

func TestReadServerInfoMalformed(t *testing.T) {
	_, err := readServerInfo(recvSeq(recvStep{ev: serverInfoEvent(`{not json`)}))
	if err == nil {
		t.Fatal("a malformed $ServerInfo should surface a decode error, not silently degrade")
	}
}

func TestIsProductionNilSafe(t *testing.T) {
	var s *ServerInfo
	if s.IsProduction() {
		t.Error("nil ServerInfo must be baseline (not production)")
	}
}
