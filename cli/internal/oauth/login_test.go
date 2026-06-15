package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// loginServer builds a fake IdP whose /authorize redirects to the location
// returned by redirect(redirectURI, state), letting tests drive the callback
// outcome. The opener used with it just GETs the auth URL.
func loginServer(t *testing.T, redirect func(redirectURI, state string) string) *httptest.Server {
	t.Helper()
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
		http.Redirect(w, r, redirect(q.Get("redirect_uri"), q.Get("state")), http.StatusFound)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "x", "token_type": "Bearer", "expires_in": 3600})
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func getInBackground(authURL string) error {
	go func() {
		if resp, err := http.Get(authURL); err == nil {
			_ = resp.Body.Close()
		}
	}()
	return nil
}

func TestLoginRejectsStateMismatch(t *testing.T) {
	srv := loginServer(t, func(redirectURI, _ string) string {
		return redirectURI + "?code=authcode&state=forged"
	})
	_, err := Login(context.Background(), Config{Issuer: srv.URL, ClientID: "id"}, getInBackground)
	if err == nil || !strings.Contains(err.Error(), "state") {
		t.Fatalf("expected a state-mismatch error, got %v", err)
	}
}

func TestLoginSurfacesIdPError(t *testing.T) {
	srv := loginServer(t, func(redirectURI, state string) string {
		return redirectURI + "?error=access_denied&error_description=user+said+no&state=" + state
	})
	_, err := Login(context.Background(), Config{Issuer: srv.URL, ClientID: "id"}, getInBackground)
	if err == nil || !strings.Contains(err.Error(), "access_denied") {
		t.Fatalf("expected the IdP error to surface, got %v", err)
	}
}

func TestLoginRequiresCode(t *testing.T) {
	srv := loginServer(t, func(redirectURI, state string) string {
		return redirectURI + "?state=" + state
	})
	_, err := Login(context.Background(), Config{Issuer: srv.URL, ClientID: "id"}, getInBackground)
	if err == nil || !strings.Contains(err.Error(), "code") {
		t.Fatalf("expected a missing-code error, got %v", err)
	}
}

func TestLoginRespectsContext(t *testing.T) {
	// The opener never triggers the callback, so the flow blocks until the
	// context deadline fires - the safety valve behind the command's timeout.
	srv := loginServer(t, func(redirectURI, state string) string {
		return redirectURI + "?code=authcode&state=" + state
	})
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, err := Login(ctx, Config{Issuer: srv.URL, ClientID: "id"}, func(string) error { return nil })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
}

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
