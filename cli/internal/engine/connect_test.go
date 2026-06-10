package engine

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
)

func TestRedactConnection(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "well-formed with password",
			in:   "kurrentdb+discover://admin:supersecret@host:2113",
			want: "kurrentdb+discover://admin:***@host:2113",
		},
		{
			name: "with path and query",
			in:   "kurrentdb://admin:hunter2@host:2113/path?tls=false",
			want: "kurrentdb://admin:***@host:2113/path?tls=false",
		},
		{
			name: "no password",
			in:   "kurrentdb://admin@host:2113",
			want: "kurrentdb://admin@host:2113",
		},
		{
			name: "no userinfo",
			in:   "kurrentdb://host:2113",
			want: "kurrentdb://host:2113",
		},
		{
			name: "malformed - @ in password (best-effort)",
			in:   "kurrentdb://user:p@ss@host:2113",
			want: "kurrentdb://user:***@host:2113",
		},
		{
			name: "no scheme",
			in:   "host:2113",
			want: "host:2113",
		},
		{
			name: "empty",
			in:   "",
			want: "",
		},
		{
			name: "IPv6 host with userinfo",
			in:   "kurrentdb://user:pw@[::1]:2113",
			want: "kurrentdb://user:***@[::1]:2113",
		},
		{
			name: "IPv6 host without userinfo",
			in:   "kurrentdb://[::1]:2113",
			want: "kurrentdb://[::1]:2113",
		},
		{
			name: "gossip seeds with userinfo",
			in:   "kurrentdb://user:pw@host1:1234,host2:5678",
			want: "kurrentdb://user:***@host1:1234,host2:5678",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactConnection(tt.in)
			if got != tt.want {
				t.Errorf("RedactConnection(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if strings.Contains(got, "supersecret") || strings.Contains(got, "hunter2") {
				t.Errorf("password leaked in %q", got)
			}
		})
	}
}

func TestScrubRaw(t *testing.T) {
	raw := "kurrentdb://admin:supersecret@host:2113"
	redacted := "kurrentdb://admin:***@host:2113"
	msg := `parse "kurrentdb://admin:supersecret@host:2113": invalid URL escape "%xx"`

	got := scrubRaw(msg, raw, redacted)

	if strings.Contains(got, "supersecret") {
		t.Errorf("password leaked in scrubbed message: %q", got)
	}
	if !strings.Contains(got, redacted) {
		t.Errorf("expected redacted form in message, got %q", got)
	}
}

func TestScrubRaw_NoOpWhenRawEqualsRedacted(t *testing.T) {
	got := scrubRaw("some error: localhost", "localhost", "localhost")
	if got != "some error: localhost" {
		t.Errorf("unexpected change: %q", got)
	}
}

func TestScrubRaw_NoOpWhenRawAbsent(t *testing.T) {
	msg := "connection refused"
	got := scrubRaw(msg, "kurrentdb://user:pw@host", "kurrentdb://user:***@host")
	if got != msg {
		t.Errorf("scrubRaw should pass through when raw not in msg, got %q", got)
	}
}

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
	if _, err := Connect(connStr, dir, "prod"); err != nil && strings.Contains(err.Error(), key) {
		t.Fatalf("env overlay not applied: %v", err)
	}
	// Without an env name there's no overlay, so the variable is undefined.
	_, err := Connect(connStr, dir, "")
	if err == nil || !strings.Contains(err.Error(), key) {
		t.Fatalf("expected undefined-variable error without overlay, got %v", err)
	}
}

func TestConnect_MalformedConnStr_DoesNotLeakPassword(t *testing.T) {
	connStr := "kurrentdb://user:supersecret@host:%XX"

	_, err := Connect(connStr, "", "")
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
