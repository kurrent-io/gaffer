package engine

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/target"
)

// Connect threads its envName through to ${VAR} expansion, so a value
// from .env.<envName> resolves the connection; with no env name the
// same reference is undefined. Guards the EnvName seam end to end.
func TestConnect_AppliesEnvOverlay(t *testing.T) {
	const key = "GAFFER_CONNECT_OVERLAY_TEST"
	_ = os.Unsetenv(key)
	t.Cleanup(func() { _ = os.Unsetenv(key) })

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env.prod"), []byte(key+"=resolved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	connStr := "kurrentdb://${" + key + "}@host:2113"

	// With the prod overlay the variable resolves, so expansion does not
	// fail (any later error is the dial, not an undefined variable).
	if _, _, err := Connect(connStr, dir, "prod", nil, nil); err != nil && strings.Contains(err.Error(), key) {
		t.Fatalf("env overlay not applied: %v", err)
	}
	// Without an env name there's no overlay, so the variable is undefined.
	_, _, err := Connect(connStr, dir, "", nil, nil)
	if err == nil || !strings.Contains(err.Error(), key) {
		t.Fatalf("expected undefined-variable error without overlay, got %v", err)
	}
}

// A user certificate is presented in the TLS handshake, so a connection with
// TLS disabled can't use one; Connect rejects the combination before dialing.
func TestConnect_CertRequiresTLS(t *testing.T) {
	cert := &config.CertAuth{CertFile: "user.crt", KeyFile: "user.key"}
	_, _, err := Connect("kurrentdb://host:2113?tls=false", t.TempDir(), "", nil, cert)
	if err == nil || !strings.Contains(err.Error(), "requires TLS") {
		t.Fatalf("expected a TLS-required error, got %v", err)
	}
}

func TestConnect_MalformedConnStr_DoesNotLeakPassword(t *testing.T) {
	connStr := "kurrentdb://user:supersecret@host:%XX"

	_, _, err := Connect(connStr, "", "", nil, nil)
	if err == nil {
		t.Fatal("expected error for malformed connection string")
	}
	if strings.Contains(err.Error(), "supersecret") {
		t.Errorf("password leaked in error: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "user:***@") {
		t.Errorf("expected redacted form in error, got %q", err.Error())
	}
}

type fakeVersionProvider struct {
	v   *kurrentdb.ServerVersion
	err error
}

func (f fakeVersionProvider) GetServerVersion() (*kurrentdb.ServerVersion, error) {
	return f.v, f.err
}

func TestProbeServerVersion(t *testing.T) {
	cases := []struct {
		name string
		v    *kurrentdb.ServerVersion
		err  error
		want string
	}{
		{"happy", &kurrentdb.ServerVersion{Major: 26, Minor: 1, Patch: 0}, nil, "26.1"},
		{"major-only", &kurrentdb.ServerVersion{Major: 27, Minor: 0}, nil, "27.0"},
		{"error", nil, errors.New("dial timeout"), "unknown"},
		{"nil-version", nil, nil, "unknown"},
		{"zero-version", &kurrentdb.ServerVersion{}, nil, "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ProbeServerVersion(fakeVersionProvider{v: tc.v, err: tc.err})
			if got != tc.want {
				t.Errorf("ProbeServerVersion = %q, want %q", got, tc.want)
			}
		})
	}
}

// oauthFakeIDP serves OIDC discovery + a token endpoint that echoes the grant
// type into the access token, for testing oauthProvider without a real IdP.
func oauthFakeIDP(t *testing.T) *httptest.Server {
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
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "access-" + r.FormValue("grant_type"),
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// A configured client secret selects the client-credentials grant and never
// opens the keyring, so this exercises the engine-level wiring without a store.
func TestOAuthProvider_ClientCredentials(t *testing.T) {
	srv := oauthFakeIDP(t)

	provider, err := oauthProvider(target.Target{
		Env:               "prod",
		OAuth:             &config.OAuthConfig{Issuer: srv.URL, ClientID: "id"},
		OAuthClientSecret: "secret",
	}, &AuthInvalidation{})
	if err != nil {
		t.Fatalf("oauthProvider: %v", err)
	}
	creds, err := provider(context.Background())
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	if !strings.Contains(creds.BearerToken, "client_credentials") {
		t.Errorf("expected a client_credentials bearer token, got %q", creds.BearerToken)
	}
}

func TestConnectionLost(t *testing.T) {
	l := &liveSource{cfg: LiveSourceConfig{EnvName: "prod"}}

	// No flag, or a flag that wasn't tripped: a plain disconnect.
	for _, inv := range []*AuthInvalidation{nil, {}} {
		err := l.connectionLost(inv, "subscription dropped")
		if !errors.Is(err, ErrDBDisconnect) {
			t.Errorf("expected ErrDBDisconnect, got %v", err)
		}
		if are := (*AuthRequiredError)(nil); errors.As(err, &are) {
			t.Errorf("did not expect AuthRequiredError, got %v", err)
		}
	}

	// Tripped (token rejected mid-run): re-sign-in is required.
	tripped := &AuthInvalidation{}
	tripped.Trip()
	err := l.connectionLost(tripped, "subscription dropped")
	var are *AuthRequiredError
	if !errors.As(err, &are) || are.Env != "prod" {
		t.Errorf("expected AuthRequiredError for prod, got %v", err)
	}
}
