package testutil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// FakeIDP serves OIDC discovery plus a token endpoint that echoes the grant
// type into the access token ("access-<grant_type>"), for exercising OAuth
// flows without a real identity provider. TokenHits counts /token requests,
// so a test can assert laziness (zero before first use) and memoization
// (one after many concurrent uses).
type FakeIDP struct {
	*httptest.Server
	TokenHits atomic.Int64
}

// NewFakeIDP starts the fake and registers its shutdown with t.Cleanup.
func NewFakeIDP(t *testing.T) *FakeIDP {
	t.Helper()
	f := &FakeIDP{}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 f.URL,
			"authorization_endpoint": f.URL + "/authorize",
			"token_endpoint":         f.URL + "/token",
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		f.TokenHits.Add(1)
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "access-" + r.FormValue("grant_type"),
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}
