package oauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestLogin drives the full authorization-code + PKCE flow: a fake IdP whose
// /authorize redirects to the loopback callback, and an opener that just GETs
// the authorization URL (following the redirect into gaffer's listener).
func TestLogin(t *testing.T) {
	mux := http.NewServeMux()
	var srv *httptest.Server

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 srv.URL,
			"authorization_endpoint": srv.URL + "/authorize",
			"token_endpoint":         srv.URL + "/token",
		})
	})
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("code_challenge") == "" || q.Get("code_challenge_method") != "S256" {
			http.Error(w, "missing PKCE challenge", http.StatusBadRequest)
			return
		}
		http.Redirect(w, r, q.Get("redirect_uri")+"?code=authcode&state="+q.Get("state"), http.StatusFound)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.FormValue("code") != "authcode" || r.FormValue("code_verifier") == "" {
			http.Error(w, "bad code or missing PKCE verifier", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "login-access",
			"token_type":    "Bearer",
			"expires_in":    3600,
			"refresh_token": "login-refresh",
		})
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()

	opener := func(authURL string) error {
		go func() {
			if resp, err := http.Get(authURL); err == nil {
				_ = resp.Body.Close()
			}
		}()
		return nil
	}

	tok, err := Login(context.Background(), Config{Issuer: srv.URL, ClientID: "id", Scopes: []string{"openid"}}, opener)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if tok.AccessToken != "login-access" {
		t.Errorf("access token = %q, want login-access", tok.AccessToken)
	}
	if tok.RefreshToken != "login-refresh" {
		t.Errorf("refresh token = %q, want login-refresh", tok.RefreshToken)
	}
}
