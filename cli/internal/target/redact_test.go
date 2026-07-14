package target

import (
	"fmt"
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
			name: "one-slash scheme (malformed)",
			in:   "esdb:/admin:hunter2@host:2113",
			want: "esdb:/admin:***@host:2113",
		},
		{
			name: "no scheme with userinfo",
			in:   "admin:hunter2@host:2113",
			want: "admin:***@host:2113",
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

	got := ScrubConnection(msg, raw)

	if strings.Contains(got, "supersecret") {
		t.Errorf("password leaked in scrubbed message: %q", got)
	}
	if !strings.Contains(got, redacted) {
		t.Errorf("expected redacted form in message, got %q", got)
	}
}

func TestScrubConnection_FragmentEcho(t *testing.T) {
	// The kurrentdb parser echoes fragments of the input (the URL path, a
	// host segment) that never match the whole connection string; the
	// password's ":pw@" context still survives in them.
	raw := "esdb:/admin:hunter2@host:2113"
	got := ScrubConnection("unsupported URL path: /admin:hunter2@host:2113", raw)
	if strings.Contains(got, "hunter2") {
		t.Errorf("password leaked in scrubbed fragment echo: %q", got)
	}
	if !strings.Contains(got, ":***@") {
		t.Errorf("expected masked userinfo in %q", got)
	}
}

func TestScrubConnection_QuotedEcho(t *testing.T) {
	// url errors %q-quote their input, so a password with a quote or a
	// control character is spelled with escapes in the echo.
	raw := "esdb://admin:hu\"nter2@host\x01:2113"
	msg := fmt.Sprintf("parse %q: net/url: invalid control character in URL", raw)
	got := ScrubConnection(msg, raw)
	if strings.Contains(got, "nter2") {
		t.Errorf("password leaked in scrubbed quoted echo: %q", got)
	}
	if !strings.Contains(got, ":***@") {
		t.Errorf("expected masked userinfo in %q", got)
	}
}

func TestScrubRaw_NoOpWhenRawEqualsRedacted(t *testing.T) {
	got := ScrubConnection("some error: localhost", "localhost")
	if got != "some error: localhost" {
		t.Errorf("unexpected change: %q", got)
	}
}

func TestScrubRaw_NoOpWhenRawAbsent(t *testing.T) {
	msg := "connection refused"
	got := ScrubConnection(msg, "kurrentdb://user:pw@host")
	if got != msg {
		t.Errorf("scrubRaw should pass through when raw not in msg, got %q", got)
	}
}
