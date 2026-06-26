package oauth

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/99designs/keyring"
	"golang.org/x/oauth2"
)

// fakeIDP serves OIDC discovery plus a token endpoint that echoes the grant
// type into the access token, so tests can assert which grant was used.
func fakeIDP(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	var srv *httptest.Server
	calls := 0

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 srv.URL,
			"authorization_endpoint": srv.URL + "/authorize",
			"token_endpoint":         srv.URL + "/token",
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  fmt.Sprintf("access-%d-%s", calls, r.FormValue("grant_type")),
			"token_type":    "Bearer",
			"expires_in":    3600,
			"refresh_token": "refresh-rotated",
		})
	})

	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestDiscover(t *testing.T) {
	srv := fakeIDP(t)
	eps, err := Discover(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if eps.TokenEndpoint != srv.URL+"/token" || eps.AuthorizationEndpoint != srv.URL+"/authorize" {
		t.Errorf("unexpected endpoints: %+v", eps)
	}
}

func TestDiscoverError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	if _, err := Discover(context.Background(), srv.URL); err == nil {
		t.Fatal("expected error for missing discovery document")
	}
}

func TestDiscoverRejectsInsecureEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"authorization_endpoint": "http://idp.example.com/authorize",
			"token_endpoint":         "http://idp.example.com/token",
		})
	}))
	defer srv.Close()
	if _, err := Discover(context.Background(), srv.URL); err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("expected an https-endpoint error, got %v", err)
	}
}

func TestDiscoverRejectsIssuerMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 "https://attacker.example.com",
			"authorization_endpoint": "https://attacker.example.com/authorize",
			"token_endpoint":         "https://attacker.example.com/token",
		})
	}))
	defer srv.Close()
	if _, err := Discover(context.Background(), srv.URL); err == nil || !strings.Contains(err.Error(), "issuer") {
		t.Fatalf("expected an issuer-mismatch error, got %v", err)
	}
}

func TestWithHTTPClient(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// The server is signed by its own (untrusted) cert; write it as a CA bundle.
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(caFile, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}

	get := func(ctx context.Context) error {
		resp, err := ctx.Value(oauth2.HTTPClient).(*http.Client).Get(srv.URL)
		if err != nil {
			return err
		}
		return resp.Body.Close()
	}

	t.Run("trusts the configured CA", func(t *testing.T) {
		ctx, err := WithHTTPClient(context.Background(), 5*time.Second, caFile)
		if err != nil {
			t.Fatalf("WithHTTPClient: %v", err)
		}
		if err := get(ctx); err != nil {
			t.Errorf("expected the configured CA to be trusted, got %v", err)
		}
	})

	t.Run("rejects an untrusted cert without a CA", func(t *testing.T) {
		ctx, err := WithHTTPClient(context.Background(), 5*time.Second, "")
		if err != nil {
			t.Fatalf("WithHTTPClient: %v", err)
		}
		if err := get(ctx); err == nil {
			t.Error("expected an untrusted-certificate error")
		}
	})

	t.Run("errors on an unreadable ca_file", func(t *testing.T) {
		if _, err := WithHTTPClient(context.Background(), time.Second, "/no/such/ca.pem"); err == nil {
			t.Error("expected an error for a missing ca_file")
		}
	})
}

func TestResolveCAFile(t *testing.T) {
	if got := ResolveCAFile("", "/root"); got != "" {
		t.Errorf("empty stays empty, got %q", got)
	}
	if got := ResolveCAFile("/abs/ca.pem", "/root"); got != "/abs/ca.pem" {
		t.Errorf("absolute unchanged, got %q", got)
	}
	if got := ResolveCAFile("certs/ca.pem", "/root"); got != "/root/certs/ca.pem" {
		t.Errorf("relative joined to root, got %q", got)
	}
}

func TestTokenSourceClientCredentials(t *testing.T) {
	srv := fakeIDP(t)
	ts, err := TokenSource(context.Background(), Config{Issuer: srv.URL, ClientID: "id", Scopes: []string{"openid"}}, "secret", nil)
	if err != nil {
		t.Fatalf("TokenSource: %v", err)
	}
	tok, err := ts.Token()
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if !strings.Contains(tok.AccessToken, "client_credentials") {
		t.Errorf("expected client_credentials grant, got token %q", tok.AccessToken)
	}
}

func TestTokenSourceInteractiveRefreshesAndPersists(t *testing.T) {
	srv := fakeIDP(t)
	store := newTokenStore(keyring.NewArrayKeyring(nil))
	id := Identity(srv.URL, "id")

	// An expired access token with a refresh token forces a refresh on first use.
	if err := store.Save(id, &oauth2.Token{
		AccessToken:  "stale",
		RefreshToken: "refresh-old",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	ts, err := TokenSource(context.Background(), Config{Issuer: srv.URL, ClientID: "id"}, "", store)
	if err != nil {
		t.Fatalf("TokenSource: %v", err)
	}
	tok, err := ts.Token()
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if !strings.Contains(tok.AccessToken, "refresh_token") {
		t.Errorf("expected refresh_token grant, got %q", tok.AccessToken)
	}

	stored, err := store.Load(id)
	if err != nil {
		t.Fatalf("load after refresh: %v", err)
	}
	if stored.AccessToken != tok.AccessToken {
		t.Errorf("refreshed token not persisted: stored %q, want %q", stored.AccessToken, tok.AccessToken)
	}
}

func TestTokenSourceInteractiveRequiresLogin(t *testing.T) {
	srv := fakeIDP(t)
	store := newTokenStore(keyring.NewArrayKeyring(nil))
	_, err := TokenSource(context.Background(), Config{Issuer: srv.URL, ClientID: "id"}, "", store)
	if !errors.Is(err, ErrNoToken) {
		t.Fatalf("expected ErrNoToken, got %v", err)
	}
}

// TestPersistingSourceConcurrent exercises the per-RPC concurrency the provider
// promises. Run with -race to catch a data race in persistingSource.
func TestPersistingSourceConcurrent(t *testing.T) {
	srv := fakeIDP(t)
	store := newTokenStore(keyring.NewArrayKeyring(nil))
	id := Identity(srv.URL, "id")
	if err := store.Save(id, &oauth2.Token{
		AccessToken:  "stale",
		RefreshToken: "refresh-old",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	ts, err := TokenSource(context.Background(), Config{Issuer: srv.URL, ClientID: "id"}, "", store)
	if err != nil {
		t.Fatalf("TokenSource: %v", err)
	}

	const n = 50
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for range n {
		wg.Go(func() {
			if _, err := ts.Token(); err != nil {
				errs <- err
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent Token(): %v", err)
	}
}

func TestIsInvalidGrant(t *testing.T) {
	if !IsInvalidGrant(&oauth2.RetrieveError{ErrorCode: "invalid_grant"}) {
		t.Error("expected true for invalid_grant")
	}
	// Survives wrapping (the refresh error reaches us wrapped).
	if !IsInvalidGrant(fmt.Errorf("refresh: %w", &oauth2.RetrieveError{ErrorCode: "invalid_grant"})) {
		t.Error("expected true for a wrapped invalid_grant")
	}
	if IsInvalidGrant(&oauth2.RetrieveError{ErrorCode: "invalid_client"}) {
		t.Error("expected false for a different error code")
	}
	if IsInvalidGrant(errors.New("network down")) {
		t.Error("expected false for a non-oauth error")
	}
	if IsInvalidGrant(nil) {
		t.Error("expected false for nil")
	}
}
