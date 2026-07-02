package remote

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestParseDefinition(t *testing.T) {
	for _, tc := range []struct {
		name string
		json string
		want Definition
	}{
		{
			name: "full definition",
			json: `{"query":"fromAll()","engineVersion":2,"mode":"Continuous","emitEnabled":true,"trackEmittedStreams":true,"enabled":true}`,
			want: Definition{Query: "fromAll()", EngineVersion: 2, Mode: "Continuous", Emit: true, TrackEmittedStreams: true, Enabled: true},
		},
		{
			name: "absent enabled means disabled (omitted-when-false on the wire)",
			json: `{"query":"q"}`,
			want: Definition{Query: "q", EngineVersion: 1, Enabled: false},
		},
		{
			name: "omitted fields take documented defaults",
			json: `{"query":"fromAll()"}`,
			want: Definition{Query: "fromAll()", EngineVersion: 1}, // absent engineVersion => 1, bools => false
		},
		{
			name: "explicit emit false",
			json: `{"query":"q","emitEnabled":false}`,
			want: Definition{Query: "q", EngineVersion: 1, Emit: false},
		},
		{
			name: "explicit engine version 1 matches the absent default",
			json: `{"query":"q","engineVersion":1}`,
			want: Definition{Query: "q", EngineVersion: 1},
		},
		{
			name: "empty object is a valid V1 definition, not a tombstone",
			json: `{}`,
			want: Definition{EngineVersion: 1},
		},
		{
			// The server serialises camelCase, but Go unmarshals case-insensitively,
			// so a casing change wouldn't silently drop fields.
			name: "pascal case still decodes",
			json: `{"Query":"q","EngineVersion":2,"EmitEnabled":true}`,
			want: Definition{Query: "q", EngineVersion: 2, Emit: true},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseDefinition([]byte(tc.json), "p")
			if err != nil {
				t.Fatalf("parseDefinition: %v", err)
			}
			if *got != tc.want {
				t.Fatalf("got %+v, want %+v", *got, tc.want)
			}
		})
	}
}

func TestParseDefinitionTombstoneIsNotFound(t *testing.T) {
	for _, body := range []string{`{"query":"q","deleted":true}`, `{"query":"q","deleting":true}`} {
		if _, err := parseDefinition([]byte(body), "p"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%s: want ErrNotFound, got %v", body, err)
		}
	}
}

func TestParseDefinitionBadJSON(t *testing.T) {
	for _, body := range []string{"{not json", ""} { // malformed, and empty event data
		_, err := parseDefinition([]byte(body), "p")
		if err == nil || errors.Is(err, ErrNotFound) {
			t.Fatalf("%q: want a decode error, got %v", body, err)
		}
	}
}

func updatedEvent(data string) *kurrentdb.ResolvedEvent {
	return &kurrentdb.ResolvedEvent{Event: &kurrentdb.RecordedEvent{EventType: projectionUpdatedType, Data: []byte(data)}}
}

type recvStep struct {
	ev  *kurrentdb.ResolvedEvent
	err error
}

func recvSeq(steps ...recvStep) func() (*kurrentdb.ResolvedEvent, error) {
	i := 0
	return func() (*kurrentdb.ResolvedEvent, error) {
		if i >= len(steps) {
			return nil, io.EOF
		}
		s := steps[i]
		i++
		return s.ev, s.err
	}
}

func TestReadDefinitionReturnsLatestState(t *testing.T) {
	// Newest-first: the first $ProjectionUpdated wins; a later (older) one is
	// never reached. Two differing events prove it returns the newest, not the last.
	next := recvSeq(
		recvStep{ev: updatedEvent(`{"query":"newest","engineVersion":2}`)},
		recvStep{ev: updatedEvent(`{"query":"older","engineVersion":1}`)},
	)
	got, err := readDefinition(next, "orders")
	if err != nil {
		t.Fatalf("readDefinition: %v", err)
	}
	if got.Query != "newest" || got.EngineVersion != 2 {
		t.Fatalf("got %+v, want the newest event", got)
	}
}

func TestReadDefinitionSkipsNonStateEvents(t *testing.T) {
	other := &kurrentdb.ResolvedEvent{Event: &kurrentdb.RecordedEvent{EventType: "$metadata"}}
	linkOnly := &kurrentdb.ResolvedEvent{Event: nil} // resolved link with no event
	next := recvSeq(
		recvStep{ev: other},
		recvStep{ev: nil},      // degenerate (nil, nil): must skip, not panic
		recvStep{ev: linkOnly}, // resolved link with no underlying event
		recvStep{ev: updatedEvent(`{"query":"q","engineVersion":1}`)},
	)
	got, err := readDefinition(next, "orders")
	if err != nil {
		t.Fatalf("readDefinition: %v", err)
	}
	if got.Query != "q" {
		t.Fatalf("got %+v", got)
	}
}

func TestReadDefinitionCapturesEventTime(t *testing.T) {
	// The last-write time comes from the event's CreatedDate, so it's available
	// even for a projection carrying no tool metadata.
	when := time.Date(2026, 6, 29, 9, 38, 0, 0, time.UTC)
	ev := &kurrentdb.ResolvedEvent{Event: &kurrentdb.RecordedEvent{EventType: projectionUpdatedType, Data: []byte(`{"query":"q"}`), CreatedDate: when}}
	got, err := readDefinition(recvSeq(recvStep{ev: ev}), "orders")
	if err != nil {
		t.Fatalf("readDefinition: %v", err)
	}
	if !got.Time.Equal(when) {
		t.Fatalf("Time = %v, want the event CreatedDate %v", got.Time, when)
	}
}

func TestReadDefinitionEmptyStreamIsNotFound(t *testing.T) {
	next := recvSeq() // immediate EOF
	if _, err := readDefinition(next, "orders"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestReadDefinitionClassifiesReadError(t *testing.T) {
	next := recvSeq(recvStep{err: status.New(codes.NotFound, "stream not found").Err()})
	if _, err := readDefinition(next, "orders"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
