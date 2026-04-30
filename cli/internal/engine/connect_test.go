package engine

import (
	"strings"
	"testing"
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

func TestConnect_MalformedConnStr_DoesNotLeakPassword(t *testing.T) {
	connStr := "kurrentdb://user:supersecret@host:%XX"

	_, err := Connect(connStr, "")
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
