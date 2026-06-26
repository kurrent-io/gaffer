package lsp

import (
	"testing"

	"github.com/sourcegraph/jsonrpc2"
)

func TestStats_ZeroBeforeActivity(t *testing.T) {
	s := NewServer(ServerOptions{})
	got := s.Stats()
	if got.CodeLensRequestCount != 0 {
		t.Errorf("CodeLensRequestCount = %d, want 0", got.CodeLensRequestCount)
	}
	if got.DiagnosticPublishCount != 0 {
		t.Errorf("DiagnosticPublishCount = %d, want 0", got.DiagnosticPublishCount)
	}
}

func TestHandleCodeLens_IncrementsCounter(t *testing.T) {
	s := NewServer(ServerOptions{})
	// nil params path: handler returns []CodeLens{} early. The
	// counter bump must precede the early-return so requests with
	// empty params still appear in the data.
	for i := range 3 {
		if _, err := s.handleCodeLens(&jsonrpc2.Request{}); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if got := s.Stats().CodeLensRequestCount; got != 3 {
		t.Errorf("CodeLensRequestCount = %d, want 3", got)
	}
}

func TestPublishDiagnostics_IncrementsCounter(t *testing.T) {
	s := NewServer(ServerOptions{})
	// No client connection - publishDiagnostics drops the
	// notification but the counter still records the attempt.
	// "DiagnosticPublishCount" reflects parse-pipeline activity,
	// not bytes-on-wire.
	for range 2 {
		s.publishDiagnostics("file:///does/not/matter.toml", nil)
	}
	if got := s.Stats().DiagnosticPublishCount; got != 2 {
		t.Errorf("DiagnosticPublishCount = %d, want 2", got)
	}
}
