package remote

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNodeOptionsEndpoint(t *testing.T) {
	for _, tc := range []struct {
		name       string
		connection string
		wantURL    string
		wantUser   string
		wantPass   string
		wantSkip   bool
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
			endpoint, user, pass, skip, err := nodeOptionsEndpoint(tc.connection)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want an error, got %q", endpoint)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if endpoint != tc.wantURL || user != tc.wantUser || pass != tc.wantPass || skip != tc.wantSkip {
				t.Errorf("got (%q, %q, %q, %v), want (%q, %q, %q, %v)",
					endpoint, user, pass, skip, tc.wantURL, tc.wantUser, tc.wantPass, tc.wantSkip)
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
		node, err := FetchNodeOptions(context.Background(), conn, "", "")
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
		if _, err := FetchNodeOptions(context.Background(), conn, "envuser", "envpass"); err != nil {
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
		if _, err := FetchNodeOptions(context.Background(), conn, "", ""); err == nil {
			t.Error("a 401 should surface as an error for the caller to skip on")
		}
	})
}
