package target

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/envvar"
	"github.com/kurrent-io/gaffer/cli/internal/testutil"
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

// The OAuth target's lazy bearer source: a client secret from the overlay
// selects the client-credentials grant without a keyring.
func TestResolve_BearerTokenClientCredentials(t *testing.T) {
	clearCreds(t)
	idp := testutil.NewFakeIDP(t)
	root := t.TempDir()
	writeEnvFile(t, root, ".env.production", "KURRENTDB_OAUTH_CLIENT_SECRET=s3cret\n")

	tgt, err := Resolve(root, config.ResolvedEnv{
		Name:       "production",
		Connection: "kurrentdb://x:1",
		OAuth:      &config.OAuthConfig{Issuer: idp.URL, ClientID: "svc"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if tgt.BearerToken == nil {
		t.Fatal("BearerToken should be set on an OAuth target")
	}
	tok, err := tgt.BearerToken(context.Background())
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

// The bearer accessor's three structural promises: Resolve does no I/O
// (laziness), the source is built once (memoization), and concurrent calls
// are safe (the race detector watches the sync.Once path).
func TestResolve_BearerTokenLazyMemoizedConcurrent(t *testing.T) {
	clearCreds(t)
	idp := testutil.NewFakeIDP(t)
	root := t.TempDir()
	writeEnvFile(t, root, ".env.production", "KURRENTDB_OAUTH_CLIENT_SECRET=s3cret\n")

	tgt, err := Resolve(root, config.ResolvedEnv{
		Name:       "production",
		Connection: "kurrentdb://x:1",
		OAuth:      &config.OAuthConfig{Issuer: idp.URL, ClientID: "svc"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := idp.TokenHits.Load(); got != 0 {
		t.Fatalf("Resolve performed %d token fetches, want none (lazy)", got)
	}

	var wg sync.WaitGroup
	tokens := make([]string, 4)
	errs := make([]error, 4)
	for i := range tokens {
		wg.Go(func() {
			tokens[i], errs[i] = tgt.BearerToken(context.Background())
		})
	}
	wg.Wait()
	for i := range tokens {
		if errs[i] != nil || tokens[i] != tokens[0] {
			t.Fatalf("call %d: token %q err %v, want all identical", i, tokens[i], errs[i])
		}
	}
	if got := idp.TokenHits.Load(); got != 1 {
		t.Errorf("token fetches = %d, want 1 (memoized)", got)
	}
}

// The caller's context bounds the wait even when the IdP never answers -
// the drift check's budget must hold against a hung identity provider.
func TestResolve_BearerTokenHonoursContext(t *testing.T) {
	clearCreds(t)
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-block }))
	defer func() {
		close(block)
		srv.Close()
	}()

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

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tgt.BearerToken(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// The structural guard for the load-order precondition: a base .env that
// exists but was never envvar.Load-ed must refuse resolution loudly rather
// than silently produce empty credentials (the UI-1820 shape).
func TestResolve_RefusesUnloadedBaseEnv(t *testing.T) {
	clearCreds(t)
	root := t.TempDir()
	writeEnvFile(t, root, ".env", "KURRENTDB_USERNAME=baseuser\n")

	if _, err := Resolve(root, config.ResolvedEnv{Connection: "kurrentdb://x:1"}); err == nil || !strings.Contains(err.Error(), "never loaded") {
		t.Fatalf("err = %v, want the unloaded-.env refusal", err)
	}

	if err := envvar.Load(root); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(root, config.ResolvedEnv{Connection: "kurrentdb://x:1"}); err != nil {
		t.Fatalf("Resolve after Load: %v", err)
	}
}

// An env may use mutual TLS and OAuth together (engine documents the
// combination); the target carries both.
func TestResolve_CertAndOAuthCombine(t *testing.T) {
	clearCreds(t)
	root := t.TempDir()
	writeEnvFile(t, root, ".env.production", "KURRENTDB_OAUTH_CLIENT_SECRET=s3cret\n")

	tgt, err := Resolve(root, config.ResolvedEnv{
		Name:       "production",
		Connection: "kurrentdb://x:1",
		OAuth:      &config.OAuthConfig{ClientID: "svc"},
		Cert:       &config.CertAuth{CertFile: "/abs/user.crt", KeyFile: "/abs/user.key"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if tgt.BearerToken == nil || tgt.CertFile != "/abs/user.crt" || tgt.KeyFile != "/abs/user.key" {
		t.Errorf("target = %+v, want bearer and cert both carried", tgt)
	}
}
