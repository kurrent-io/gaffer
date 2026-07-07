package target

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/config"
)

// clearCreds makes the credential variables absent (not empty) for the test:
// shell env outranks the overlay, so a stray login in the test environment
// would mask the fixtures. t.Setenv registers restoration.
func clearCreds(t *testing.T) {
	t.Helper()
	for _, v := range []string{"KURRENTDB_USERNAME", "KURRENTDB_PASSWORD", "KURRENTDB_OAUTH_CLIENT_SECRET"} {
		t.Setenv(v, "")
		_ = os.Unsetenv(v)
	}
}

func writeEnvFile(t *testing.T, root, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestResolve_ExpandsConnectionFromOverlay(t *testing.T) {
	clearCreds(t)
	root := t.TempDir()
	writeEnvFile(t, root, ".env.staging", "DB_HOST=db.example:2113\n")

	tgt, err := Resolve(root, config.ResolvedEnv{Name: "staging", Connection: "kurrentdb://${DB_HOST}?tls=false"})
	if err != nil {
		t.Fatal(err)
	}
	if tgt.Connection != "kurrentdb://db.example:2113?tls=false" || tgt.Env != "staging" {
		t.Errorf("target = %+v", tgt)
	}
}

func TestResolve_CredentialsFromOverlay(t *testing.T) {
	clearCreds(t)
	root := t.TempDir()
	writeEnvFile(t, root, ".env.production", "KURRENTDB_USERNAME=envuser\nKURRENTDB_PASSWORD=envpass\n")

	tgt, err := Resolve(root, config.ResolvedEnv{Name: "production", Connection: "kurrentdb://x:1"})
	if err != nil {
		t.Fatal(err)
	}
	if tgt.Username != "envuser" || tgt.Password != "envpass" {
		t.Errorf("credentials = %q/%q, want the overlay's", tgt.Username, tgt.Password)
	}
}

func TestResolve_ShellEnvOutranksOverlay(t *testing.T) {
	clearCreds(t)
	t.Setenv("KURRENTDB_USERNAME", "shelluser")
	root := t.TempDir()
	writeEnvFile(t, root, ".env.production", "KURRENTDB_USERNAME=envuser\n")

	tgt, err := Resolve(root, config.ResolvedEnv{Name: "production", Connection: "kurrentdb://x:1"})
	if err != nil {
		t.Fatal(err)
	}
	if tgt.Username != "shelluser" {
		t.Errorf("username = %q, want the shell's (shell > overlay)", tgt.Username)
	}
}

func TestResolve_OAuthIgnoresBasicCredsAndResolvesSecret(t *testing.T) {
	clearCreds(t)
	root := t.TempDir()
	writeEnvFile(t, root, ".env.production", "KURRENTDB_USERNAME=envuser\nKURRENTDB_OAUTH_CLIENT_SECRET=s3cret\n")

	tgt, err := Resolve(root, config.ResolvedEnv{
		Name:       "production",
		Connection: "kurrentdb://x:1",
		OAuth:      &config.OAuthConfig{ClientID: "svc"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if tgt.Username != "" || tgt.Password != "" {
		t.Errorf("basic creds = %q/%q, want ignored under OAuth", tgt.Username, tgt.Password)
	}
	if tgt.OAuthClientSecret != "s3cret" || tgt.OAuth == nil {
		t.Errorf("oauth = %+v secret %q", tgt.OAuth, tgt.OAuthClientSecret)
	}
}

func TestResolve_UndefinedVarSurfaces(t *testing.T) {
	clearCreds(t)
	_, err := Resolve(t.TempDir(), config.ResolvedEnv{Name: "production", Connection: "kurrentdb://${NOPE}"})
	if err == nil || !strings.Contains(err.Error(), "NOPE") {
		t.Fatalf("err = %v, want the undefined variable named", err)
	}
}

func TestResolve_CertPaths(t *testing.T) {
	clearCreds(t)
	root := t.TempDir()
	writeEnvFile(t, root, ".env.production", "CERT_DIR=certs\n")

	tgt, err := Resolve(root, config.ResolvedEnv{
		Name:       "production",
		Connection: "kurrentdb://x:1",
		Cert:       &config.CertAuth{CertFile: "${CERT_DIR}/user.crt", KeyFile: "/abs/user.key"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "certs", "user.crt"); tgt.CertFile != want {
		t.Errorf("cert = %q, want relative path anchored to root %q", tgt.CertFile, want)
	}
	if tgt.KeyFile != "/abs/user.key" {
		t.Errorf("key = %q, want the absolute path unchanged", tgt.KeyFile)
	}
}

func TestResolve_EmptyCertPathRefused(t *testing.T) {
	clearCreds(t)
	root := t.TempDir()
	writeEnvFile(t, root, ".env.production", "CERT_DIR=\n")

	_, err := Resolve(root, config.ResolvedEnv{
		Name:       "production",
		Connection: "kurrentdb://x:1",
		Cert:       &config.CertAuth{CertFile: "${CERT_DIR}", KeyFile: "/abs/user.key"},
	})
	if err == nil || !strings.Contains(err.Error(), "resolved to empty") {
		t.Fatalf("err = %v, want the empty-expansion guard", err)
	}
}

func TestResolve_AdHocTargetNoOverlay(t *testing.T) {
	clearCreds(t)
	// An ad-hoc --connection target has no env name, so no overlay file is
	// read; shell credentials still apply.
	t.Setenv("KURRENTDB_USERNAME", "shelluser")
	t.Setenv("KURRENTDB_PASSWORD", "shellpass")

	tgt, err := Resolve(t.TempDir(), config.ResolvedEnv{Connection: "kurrentdb://x:1"})
	if err != nil {
		t.Fatal(err)
	}
	if tgt.Username != "shelluser" || tgt.Password != "shellpass" || tgt.Env != "" {
		t.Errorf("target = %+v", tgt)
	}
}

func TestResolveCertPath(t *testing.T) {
	t.Run("relative joins the project root", func(t *testing.T) {
		got, err := resolveCertPath("certs/user.crt", "/proj", nil)
		if err != nil || got != filepath.Join("/proj", "certs/user.crt") {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("absolute path is unchanged", func(t *testing.T) {
		abs := filepath.Join("/abs", "user.crt")
		got, err := resolveCertPath(abs, "/proj", nil)
		if err != nil || got != abs {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("expands vars before resolving", func(t *testing.T) {
		got, err := resolveCertPath("${CERT_DIR}/user.key", "/proj", map[string]string{"CERT_DIR": "sub"})
		if err != nil || got != filepath.Join("/proj", "sub/user.key") {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("undefined var errors", func(t *testing.T) {
		if _, err := resolveCertPath("${GAFFER_CERT_TEST_UNSET}/user.key", "/proj", nil); err == nil {
			t.Fatal("expected an undefined-variable error")
		}
	})
	t.Run("trims surrounding whitespace, including from expansion", func(t *testing.T) {
		got, err := resolveCertPath("  ${CERT}  ", "/proj", map[string]string{"CERT": " certs/user.crt "})
		if err != nil || got != filepath.Join("/proj", "certs/user.crt") {
			t.Fatalf("got %q, %v", got, err)
		}
	})
}

// bearerFakeIDP serves OIDC discovery + a token endpoint that echoes the
// grant type into the access token, mirroring engine's oauthFakeIDP.
func bearerFakeIDP(t *testing.T) *httptest.Server {
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

// The OAuth target's lazy bearer source: Resolve does no I/O (the fake IdP
// sees no traffic until the first BearerToken call), and a client secret
// from the overlay selects the client-credentials grant without a keyring.
func TestResolve_BearerTokenClientCredentials(t *testing.T) {
	clearCreds(t)
	srv := bearerFakeIDP(t)
	root := t.TempDir()
	writeEnvFile(t, root, ".env.production", "KURRENTDB_OAUTH_CLIENT_SECRET=s3cret\n")

	tgt, err := Resolve(root, config.ResolvedEnv{
		Name:       "production",
		Connection: "kurrentdb://x:1",
		OAuth:      &config.OAuthConfig{Issuer: srv.URL, ClientID: "svc"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if tgt.BearerToken == nil {
		t.Fatal("BearerToken should be set on an OAuth target")
	}
	tok, err := tgt.BearerToken()
	if err != nil {
		t.Fatalf("BearerToken: %v", err)
	}
	if !strings.Contains(tok, "client_credentials") {
		t.Errorf("token = %q, want the client-credentials grant", tok)
	}
}

func TestResolve_NoBearerTokenWithoutOAuth(t *testing.T) {
	clearCreds(t)
	tgt, err := Resolve(t.TempDir(), config.ResolvedEnv{Connection: "kurrentdb://x:1"})
	if err != nil {
		t.Fatal(err)
	}
	if tgt.BearerToken != nil {
		t.Error("BearerToken should be nil on a basic-auth target")
	}
}
