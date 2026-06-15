package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
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
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := ts.Token(); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent Token(): %v", err)
	}
}
