package remote

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kurrent-io/gaffer/cli/internal/target"
)

func TestNodeOptionsEndpoint(t *testing.T) {
	for _, tc := range []struct {
		name       string
		connection string
		wantURL    string
		wantUser   string
		wantPass   string
		wantSkip   bool
		wantCA     string
		wantErr    bool
	}{
		{
			name:       "insecure single host",
			connection: "kurrentdb://localhost:2114?tls=false",
			wantURL:    "http://localhost:2114/info/options",
		},
		{
			name:       "tls default with credentials",
			connection: "kurrentdb://admin:changeit@db.example:2113",
			wantURL:    "https://db.example:2113/info/options",
			wantUser:   "admin",
			wantPass:   "changeit",
		},
		{
			name:       "skip verification",
			connection: "esdb://db.example:2113?tlsVerifyCert=false",
			wantURL:    "https://db.example:2113/info/options",
			wantSkip:   true,
		},
		{
			name:       "custom verification CA",
			connection: "kurrentdb://db.example:2113?tlsCaFile=certs/ca.crt",
			wantURL:    "https://db.example:2113/info/options",
			wantCA:     "certs/ca.crt",
		},
		{
			name:       "multi-host asks the first",
			connection: "kurrentdb://a.example:2113,b.example:2113?tls=false",
			wantURL:    "http://a.example:2113/info/options",
		},
		{
			name:       "default port",
			connection: "kurrentdb://db.example?tls=false",
			wantURL:    "http://db.example:2113/info/options",
		},
		{
			name:       "ipv6 with a port",
			connection: "kurrentdb://[::1]:2114?tls=false",
			wantURL:    "http://[::1]:2114/info/options",
		},
		{
			name:       "ipv6 default port re-brackets once",
			connection: "kurrentdb://[::1]?tls=false",
			wantURL:    "http://[::1]:2113/info/options",
		},
		{
			name:       "discover scheme asks the seed",
			connection: "kurrentdb+discover://cluster.example:2113?tls=false",
			wantURL:    "http://cluster.example:2113/info/options",
		},
		{
			name:       "tls value folds case",
			connection: "kurrentdb://db.example:2113?tls=FALSE",
			wantURL:    "http://db.example:2113/info/options",
		},
		{
			name:       "no host",
			connection: "kurrentdb://",
			wantErr:    true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			endpoint, user, pass, tlsOpts, err := nodeOptionsEndpoint(tc.connection)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want an error, got %q", endpoint)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if endpoint != tc.wantURL || user != tc.wantUser || pass != tc.wantPass || tlsOpts.insecure != tc.wantSkip || tlsOpts.caFile != tc.wantCA {
				t.Errorf("got (%q, %q, %q, %+v), want (%q, %q, %q, insecure=%v ca=%q)",
					endpoint, user, pass, tlsOpts, tc.wantURL, tc.wantUser, tc.wantPass, tc.wantSkip, tc.wantCA)
			}
		})
	}
}

const sampleOptions = `[
  {"name": "Help", "value": "False", "group": "ApplicationOptions"},
  {"name": "ProjectionCompilationTimeout", "value": "500", "group": "ProjectionOptions"},
  {"name": "ProjectionExecutionTimeout", "value": "250", "group": "ProjectionOptions"},
  {"name": "MaxProjectionStateSize", "value": "16777216", "group": "ProjectionOptions"},
  {"name": "RunProjections", "value": "All", "group": "ProjectionOptions"}
]`

func TestParseNodeOptions(t *testing.T) {
	node, err := parseNodeOptions(strings.NewReader(sampleOptions))
	if err != nil {
		t.Fatal(err)
	}
	if node.CompilationTimeoutMs == nil || *node.CompilationTimeoutMs != 500 {
		t.Errorf("compilation timeout = %v, want 500", node.CompilationTimeoutMs)
	}
	if node.ExecutionTimeoutMs == nil || *node.ExecutionTimeoutMs != 250 {
		t.Errorf("execution timeout = %v, want 250", node.ExecutionTimeoutMs)
	}
	if node.MaxStateSizeBytes == nil || *node.MaxStateSizeBytes != 16777216 {
		t.Errorf("max state size = %v, want 16777216", node.MaxStateSizeBytes)
	}

	// An older server that doesn't report the options leaves the fields nil.
	node, err = parseNodeOptions(strings.NewReader(`[{"name": "Help", "value": "False"}]`))
	if err != nil || node.CompilationTimeoutMs != nil || node.MaxStateSizeBytes != nil {
		t.Errorf("absent options should stay nil, got %+v (%v)", node, err)
	}

	if _, err := parseNodeOptions(strings.NewReader("not json")); err == nil {
		t.Error("malformed payload should error")
	}
}

func TestFetchNodeOptions(t *testing.T) {
	t.Run("reads the options with basic auth", func(t *testing.T) {
		var gotAuth string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/info/options" {
				t.Errorf("path = %q, want /info/options", r.URL.Path)
			}
			gotAuth = r.Header.Get("Authorization")
			_, _ = w.Write([]byte(sampleOptions))
		}))
		defer srv.Close()

		conn := "kurrentdb://admin:changeit@" + strings.TrimPrefix(srv.URL, "http://") + "?tls=false"
		node, err := FetchNodeOptions(context.Background(), target.Target{Connection: conn})
		if err != nil {
			t.Fatal(err)
		}
		if node.MaxStateSizeBytes == nil || *node.MaxStateSizeBytes != 16777216 {
			t.Errorf("max state size = %v, want 16777216", node.MaxStateSizeBytes)
		}
		if !strings.HasPrefix(gotAuth, "Basic ") {
			t.Errorf("Authorization = %q, want the connection's basic credentials", gotAuth)
		}
	})

	t.Run("explicit credentials override the userinfo", func(t *testing.T) {
		// The .env-supplied login must win over connection-string userinfo,
		// matching the main gRPC connection's precedence (UI-1820).
		var gotAuth string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			_, _ = w.Write([]byte(sampleOptions))
		}))
		defer srv.Close()

		conn := "kurrentdb://inline:wrong@" + strings.TrimPrefix(srv.URL, "http://") + "?tls=false"
		if _, err := FetchNodeOptions(context.Background(), target.Target{Connection: conn, Username: "envuser", Password: "envpass"}); err != nil {
			t.Fatal(err)
		}
		want := "Basic " + base64.StdEncoding.EncodeToString([]byte("envuser:envpass"))
		if gotAuth != want {
			t.Errorf("Authorization = %q, want the explicit credentials %q", gotAuth, want)
		}
	})

	t.Run("an auth refusal is an error, not a payload", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		}))
		defer srv.Close()

		conn := "kurrentdb://" + strings.TrimPrefix(srv.URL, "http://") + "?tls=false"
		if _, err := FetchNodeOptions(context.Background(), target.Target{Connection: conn}); err == nil {
			t.Error("a 401 should surface as an error for the caller to skip on")
		}
	})
}

func TestFetchNodeOptions_BearerToken(t *testing.T) {
	// An OAuth target authenticates the read with its bearer token; basic
	// credentials (userinfo or resolved) don't apply.
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(sampleOptions))
	}))
	defer srv.Close()

	conn := "kurrentdb://inline:wrong@" + strings.TrimPrefix(srv.URL, "http://") + "?tls=false"
	tgt := target.Target{
		Connection:  conn,
		BearerToken: func() (string, error) { return "tok-123", nil },
	}
	if _, err := FetchNodeOptions(context.Background(), tgt); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer tok-123" {
		t.Errorf("Authorization = %q, want the bearer token", gotAuth)
	}

	// A token failure surfaces (never silently degrades to anonymous).
	tgt.BearerToken = func() (string, error) { return "", errors.New("no stored token") }
	if _, err := FetchNodeOptions(context.Background(), tgt); err == nil || !strings.Contains(err.Error(), "resolving bearer token") {
		t.Fatalf("err = %v, want the bearer failure surfaced", err)
	}
}

func TestFetchNodeOptions_UserCertificate(t *testing.T) {
	// A cert-auth target presents its user certificate in the handshake and
	// verifies the server against the connection's tlsCaFile - the whole
	// mTLS path the gRPC connection uses, on the HTTP read.
	clientCert, clientKey, clientPEM := testKeyPair(t, "gaffer-user")

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.TLS.PeerCertificates) == 0 || r.TLS.PeerCertificates[0].Subject.CommonName != "gaffer-user" {
			http.Error(w, "no client certificate", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(sampleOptions))
	}))
	clientPool := x509.NewCertPool()
	clientPool.AppendCertsFromPEM(clientPEM)
	srv.TLS = &tls.Config{ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: clientPool}
	srv.StartTLS()
	defer srv.Close()

	// The httptest server's own certificate becomes the read's tlsCaFile.
	caPath := filepath.Join(t.TempDir(), "ca.crt")
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(caPath, caPEM, 0o600); err != nil {
		t.Fatal(err)
	}

	conn := "kurrentdb://" + strings.TrimPrefix(srv.URL, "https://") + "?tlsCaFile=" + url.QueryEscape(caPath)
	node, err := FetchNodeOptions(context.Background(), target.Target{
		Connection: conn,
		CertFile:   clientCert,
		KeyFile:    clientKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	if node.MaxStateSizeBytes == nil {
		t.Error("expected the options payload through mTLS")
	}
}

// testKeyPair generates a self-signed certificate, returning the cert and
// key file paths plus the certificate PEM (for the server's client CA pool).
func testKeyPair(t *testing.T, cn string) (certPath, keyPath string, certPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	dir := t.TempDir()
	certPath, keyPath = filepath.Join(dir, cn+".crt"), filepath.Join(dir, cn+".key")
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath, certPEM
}
