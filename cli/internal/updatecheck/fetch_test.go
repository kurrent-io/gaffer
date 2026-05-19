package updatecheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNpmFetcher_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != "gaffer-cli/test" {
			t.Errorf("User-Agent = %q, want gaffer-cli/test", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q, want application/json", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"@kurrent/gaffer","version":"0.2.0"}`))
	}))
	defer srv.Close()

	f := NpmFetcher{UserAgent: "gaffer-cli/test", URL: srv.URL}
	got, err := f.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if got != "0.2.0" {
		t.Errorf("Latest = %q, want 0.2.0", got)
	}
}

func TestNpmFetcher_Non200Errors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	f := NpmFetcher{URL: srv.URL}
	_, err := f.Latest(context.Background())
	if err == nil {
		t.Fatal("Latest returned nil error on 503")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("error message %q does not mention status code", err)
	}
}

func TestNpmFetcher_MalformedJSONErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	f := NpmFetcher{URL: srv.URL}
	if _, err := f.Latest(context.Background()); err == nil {
		t.Error("Latest returned nil error on malformed JSON")
	}
}

func TestNpmFetcher_MissingVersionFieldErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"name":"@kurrent/gaffer"}`))
	}))
	defer srv.Close()

	f := NpmFetcher{URL: srv.URL}
	if _, err := f.Latest(context.Background()); err == nil {
		t.Error("Latest returned nil error when version field absent")
	}
}

func TestNpmFetcher_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(500 * time.Millisecond)
		_, _ = w.Write([]byte(`{"version":"0.2.0"}`))
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	f := NpmFetcher{URL: srv.URL}
	if _, err := f.Latest(ctx); err == nil {
		t.Error("Latest returned nil error when context expired")
	}
}
