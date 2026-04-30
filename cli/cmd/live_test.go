//go:build integration

package cmd

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	"github.com/kurrent-io/gaffer/cli/internal/testutil"
)

func TestDev_LiveSubscription(t *testing.T) {
	suffix := testutil.TestSuffix()
	connStr := testutil.ConnectionString()

	dbConfig, err := kurrentdb.ParseConnectionString(connStr)
	if err != nil {
		t.Fatal(err)
	}
	dbConfig.Logger = kurrentdb.NoopLogging()

	client, err := kurrentdb.NewClient(dbConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()

	stream := fmt.Sprintf("livetest%s-1", suffix)
	events := make([]kurrentdb.EventData, 3)
	for i := range events {
		events[i] = kurrentdb.EventData{
			EventID:     uuid.New(),
			EventType:   "Ping",
			ContentType: kurrentdb.ContentTypeJson,
			Data:        []byte(fmt.Sprintf(`{"seq":%d}`, i)),
		}
	}
	_, err = client.AppendToStream(context.Background(), stream, kurrentdb.AppendToStreamOptions{}, events...)
	if err != nil {
		t.Fatal(err)
	}

	projSource := fmt.Sprintf(`fromCategory('livetest%s')
  .foreachStream()
  .when({
    $init() { return { count: 0 }; },
    Ping(s, e) { s.count++; return s; }
  })
`, suffix)

	p := testutil.NewProject(t).
		WithConnection(connStr).
		AddProjection("counter", projSource).
		Save()
	chdirTo(t, p.Dir)

	root := NewRootCmd()
	root.SetArgs([]string{"dev", "counter", "--json", "--until-caught-up"})
	root.SetErr(&bytes.Buffer{})

	output := testutil.CaptureStdout(t, func() {
		if err := ExecuteRoot(context.Background(), root); err != nil {
			t.Fatalf("dev failed: %v", err)
		}
	})

	lines := testutil.SplitNDJSON(output)

	var eventCount int
	for _, line := range lines {
		if line["type"] == "event" {
			eventCount++
		}
	}
	if eventCount < 3 {
		t.Errorf("event lines: got %d, want at least 3", eventCount)
	}

	if len(lines) == 0 {
		t.Fatal("no output")
	}
	last := lines[len(lines)-1]
	if last["type"] != "summary" {
		t.Errorf("expected summary as last line, got type=%v", last["type"])
	}
}
