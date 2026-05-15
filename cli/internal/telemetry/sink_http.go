package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultWorkerURL is where the Cloudflare worker ingests envelopes.
// Default is the staging worker so unreleased builds (run from source,
// `go run`, locally-built binaries, CI test artefacts) don't pollute
// the production PostHog project. Release tooling injects the prod URL
// via ldflags (see cli/justfile#build-release):
//
//	go build -ldflags "-X github.com/kurrent-io/gaffer/cli/internal/telemetry.DefaultWorkerURL=https://telemetry.gaffer.kurrent.io/v1/ingest"
//
// The staging URL is also hard-coded in editors/vscode/vite.config.ts
// (as `STAGING_INGEST_URL`); keep them in lockstep if the worker URL
// ever moves.
//
// Versioned in the URL path so additive schema changes don't require a
// new URL; breaking changes get /v2/ingest and run alongside during a
// deprecation window.
var DefaultWorkerURL = "https://gaffer-telemetry-staging.kurrent.workers.dev/v1/ingest"

// defaultUserAgent is the User-Agent when the caller doesn't set one.
// Real builds override it via WithUserAgent so the worker can attribute
// traffic per release.
const defaultUserAgent = "gaffer-cli/unknown"

// httpSink POSTs envelopes to a worker URL. Connection pooling is enabled
// so back-to-back emits in a long session reuse the TLS handshake; Close
// drops idle conns at process exit.
type httpSink struct {
	url       string
	userAgent string
	client    *http.Client
}

func newHTTPSink(url, userAgent string) *httpSink {
	return &httpSink{
		url:       url,
		userAgent: userAgent,
		client: &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:        4,
				MaxIdleConnsPerHost: 4,
				IdleConnTimeout:     10 * time.Second,
			},
		},
	}
}

func (s *httpSink) Send(ctx context.Context, env *Envelope) error {
	body, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshal envelope: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", s.userAgent)
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Worker may return diagnostic text on validation failures; read
		// the first 256 bytes so the error is actionable rather than
		// just an HTTP code.
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		if len(snippet) > 0 {
			return fmt.Errorf("http %d: %s", resp.StatusCode, bytes.TrimSpace(snippet))
		}
		return fmt.Errorf("http %d", resp.StatusCode)
	}
	// Drain so the connection returns to the pool cleanly.
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// Close releases any pooled connections so the process can exit cleanly
// without waiting on IdleConnTimeout. In-flight requests (if any) hold
// their own references and are not affected.
func (s *httpSink) Close(_ context.Context) error {
	if t, ok := s.client.Transport.(*http.Transport); ok {
		t.CloseIdleConnections()
	}
	return nil
}
