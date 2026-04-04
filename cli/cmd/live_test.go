//go:build integration

package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	"github.com/kurrent-io/gaffer/cli/internal/config"
)

func TestDev_LiveSubscription(t *testing.T) {
	suffix := strings.ReplaceAll(uuid.New().String(), "-", "")[:12]

	connStr := "kurrentdb://localhost:2113?tls=false"
	if s := os.Getenv("KURRENTDB_URL"); s != "" {
		connStr = s
	}

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

	dir := t.TempDir()
	projDir := filepath.Join(dir, "projections")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}

	projSource := fmt.Sprintf(`fromCategory('livetest%s')
  .foreachStream()
  .when({
    $init: function() { return { count: 0 }; },
    Ping: function(s, e) { s.count++; return s; }
  })
`, suffix)
	if err := os.WriteFile(filepath.Join(projDir, "counter.js"), []byte(projSource), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Connection: connStr,
		Projection: []config.Projection{
			{Name: "counter", Entry: "projections/counter.js"},
		},
	}
	if err := config.Save(filepath.Join(dir, "gaffer.toml"), cfg); err != nil {
		t.Fatal(err)
	}

	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	devJSON = false
	devEvents = ""
	devConnection = ""
	devDebug = false
	devDebugPort = 4711
	rootCmd.SetArgs([]string{"dev", "counter", "--json"})
	rootCmd.SetErr(&bytes.Buffer{})

	// The live subscription runs until interrupted.
	// Run in a goroutine, send SIGINT after a delay.
	r, w, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatal(pipeErr)
	}
	origStdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = origStdout }()

	done := make(chan error, 1)
	go func() {
		done <- rootCmd.Execute()
	}()

	// Give the subscription time to catch up and process events
	time.Sleep(3 * time.Second)
	_ = syscall.Kill(syscall.Getpid(), syscall.SIGINT)

	select {
	case err = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("command did not exit after SIGINT")
	}

	_ = w.Close()

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	output := buf.String()

	// The command should exit cleanly on interrupt (no error, or context cancelled)
	if err != nil && !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := splitNDJSON(output)

	// Should have processed some events before we interrupted
	var eventCount int
	for _, line := range lines {
		if line["type"] == "event" {
			eventCount++
		}
	}
	if eventCount < 3 {
		t.Errorf("event lines: got %d, want at least 3", eventCount)
	}

	// Last line should be summary (written after interrupt)
	if len(lines) > 0 {
		last := lines[len(lines)-1]
		if last["type"] != "summary" {
			t.Errorf("expected summary as last line, got type=%v", last["type"])
		}
	}
}
