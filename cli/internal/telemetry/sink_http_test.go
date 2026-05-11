package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testEnvelope() *Envelope {
	return &Envelope{
		SchemaVersion: SchemaVersion,
		EmitterID:     "00000000-0000-0000-0000-000000000001",
		RunID:         "00000000-0000-0000-0000-000000000002",
		Context: Context{
			Emitter: EmitterCLI, LibVersion: "0.0.0", OS: OSLinux, Arch: ArchX64,
			RuntimeEnvironment: RuntimeEnvironmentLocal,
		},
	}
}

func TestHTTPSink_Success(t *testing.T) {
	var (
		received  Envelope
		gotUA     string
		gotMethod string
		gotCType  string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotCType = r.Header.Get("Content-Type")
		gotUA = r.Header.Get("User-Agent")
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := newHTTPSink(srv.URL, "gaffer-cli/1.2.3")
	env := testEnvelope()
	if err := s.Send(context.Background(), env); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if gotCType != "application/json" {
		t.Errorf("Content-Type = %s, want application/json", gotCType)
	}
	if gotUA != "gaffer-cli/1.2.3" {
		t.Errorf("User-Agent = %s, want gaffer-cli/1.2.3", gotUA)
	}
	if received.EmitterID != env.EmitterID {
		t.Errorf("received.EmitterID = %s", received.EmitterID)
	}
}

func TestHTTPSink_Non2xxReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	s := newHTTPSink(srv.URL, defaultUserAgent)
	err := s.Send(context.Background(), testEnvelope())
	if err == nil {
		t.Fatal("Send: nil error, want non-2xx error")
	}
}

func TestHTTPSink_Non2xxIncludesBodySnippet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, "schema validation failed: missing emitter_id")
	}))
	defer srv.Close()
	s := newHTTPSink(srv.URL, defaultUserAgent)
	err := s.Send(context.Background(), testEnvelope())
	if err == nil {
		t.Fatal("Send: nil error, want 400 error")
	}
	if !strings.Contains(err.Error(), "schema validation failed") {
		t.Errorf("err = %v, want body snippet in message", err)
	}
}

func TestHTTPSink_RespectsContextDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(100 * time.Millisecond):
			w.WriteHeader(http.StatusOK)
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	s := newHTTPSink(srv.URL, defaultUserAgent)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err := s.Send(ctx, testEnvelope())
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Send err = %v, want DeadlineExceeded", err)
	}
}

func TestHTTPSink_NetworkErrorBubbles(t *testing.T) {
	s := newHTTPSink("http://127.0.0.1:1/", defaultUserAgent)
	err := s.Send(context.Background(), testEnvelope())
	if err == nil {
		t.Fatal("Send: nil error, want network error")
	}
}

func TestHTTPSink_CloseIsIdempotent(t *testing.T) {
	s := newHTTPSink("http://127.0.0.1:1/", defaultUserAgent)
	if err := s.Close(context.Background()); err != nil {
		t.Errorf("Close 1: %v", err)
	}
	if err := s.Close(context.Background()); err != nil {
		t.Errorf("Close 2: %v", err)
	}
}
