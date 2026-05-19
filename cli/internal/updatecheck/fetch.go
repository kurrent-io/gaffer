package updatecheck

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// packageName is the npm name we publish under. The registry endpoint
// is derived from it; client.go also uses it in the install hint
// printed to stderr. Hard-coded rather than configurable because the
// only consumer is gaffer itself.
const packageName = "@kurrent/gaffer"

// registryURL is the npm registry metadata endpoint for the `latest`
// dist-tag. GET returns a JSON document; we only care about its
// `version` field. No script execution, no postinstall hooks.
const registryURL = "https://registry.npmjs.org/" + packageName + "/latest"

// defaultFetchTimeout bounds a single HTTP call - covers DNS, connect,
// TLS handshake, and body read. Kept close to the Flush budget set by
// main.runMain so a fetch that doesn't complete in time gets
// abandoned at exit anyway.
const defaultFetchTimeout = 2 * time.Second

// Fetcher returns the current `latest` version published to npm.
// Implementations must respect ctx; tests inject stubs.
type Fetcher interface {
	Latest(ctx context.Context) (string, error)
}

// NpmFetcher hits the public npm registry. Zero value is usable; pass
// a non-empty UserAgent so registry-side analytics can attribute traffic
// to gaffer-cli.
type NpmFetcher struct {
	UserAgent string
	// HTTPClient overrides the default. Tests inject one pointing at
	// httptest.Server. Production leaves nil for the default
	// http.Client with defaultFetchTimeout and keepalives disabled
	// (we hit the registry ~once per 24h; pooling an idle keepalive
	// for the lifetime of `gaffer dev` is wasteful).
	HTTPClient *http.Client
	// URL overrides registryURL. Tests point at httptest.Server. Empty
	// means use registryURL.
	URL string
}

func (f NpmFetcher) Latest(ctx context.Context) (string, error) {
	client := f.HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout:   defaultFetchTimeout,
			Transport: &http.Transport{DisableKeepAlives: true},
		}
	}
	url := f.URL
	if url == "" {
		url = registryURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}
	if f.UserAgent != "" {
		req.Header.Set("User-Agent", f.UserAgent)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("registry GET: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("registry GET: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}
	var payload struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("parse body: %w", err)
	}
	if payload.Version == "" {
		return "", fmt.Errorf("registry response missing version field")
	}
	return payload.Version, nil
}
