package target

import "testing"

func TestParseConnectionDiscoverAttempts(t *testing.T) {
	// Omitting maxDiscoverAttempts gets gaffer's fail-fast default, not the
	// library's 10.
	cfg, err := ParseConnection("kurrentdb://localhost:2113?tls=false")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxDiscoverAttempts != cliDiscoverAttempts {
		t.Errorf("default attempts: got %d want %d", cfg.MaxDiscoverAttempts, cliDiscoverAttempts)
	}

	// An explicit value in the connection string wins (case-insensitive).
	cfg, err = ParseConnection("kurrentdb://localhost:2113?tls=false&MaxDiscoverAttempts=9")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxDiscoverAttempts != 9 {
		t.Errorf("explicit attempts: got %d want 9", cfg.MaxDiscoverAttempts)
	}
}
