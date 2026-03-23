package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEvents_ValidArray(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.json")
	content := `[
		{"eventType":"A","streamId":"s-1","data":"{}"},
		{"eventType":"B","streamId":"s-2","data":"{}"}
	]`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	events, err := loadEvents(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
}

func TestLoadEvents_EmptyArray(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.json")
	if err := os.WriteFile(path, []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}

	events, err := loadEvents(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(events) != 0 {
		t.Fatalf("expected 0 events, got %d", len(events))
	}
}

func TestLoadEvents_NotAnArray(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.json")
	if err := os.WriteFile(path, []byte(`{"eventType":"A"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := loadEvents(path)
	if err == nil {
		t.Fatal("expected error for non-array JSON")
	}
}

func TestLoadEvents_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := loadEvents(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestLoadEvents_FileNotFound(t *testing.T) {
	_, err := loadEvents("/nonexistent/events.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
