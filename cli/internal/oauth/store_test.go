package oauth

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/99designs/keyring"
	"golang.org/x/oauth2"
)

func TestSanitizeKeyringName(t *testing.T) {
	cases := map[string]string{
		"vscode":     "vscode",
		"vs-code_1":  "vs-code_1",
		"a/b":        "ab",   // path separators dropped
		"a\\b":       "ab",   // ditto on windows-style
		"na me!":     "name", // spaces and punctuation dropped
		".":          "",     // a dot-only segment can't be a store name
		"..":         "",     // ...nor a traversal token
		"":           "",
		"my.store-2": "my.store-2",
	}
	for in, want := range cases {
		if got := sanitizeKeyringName(in); got != want {
			t.Errorf("sanitizeKeyringName(%q) = %q, want %q", in, got, want)
		}
	}
}

// GAFFER_KEYRING_NAME isolates the file-fallback store in a per-client sibling
// directory (never nested in the default, so the default's Keys/Clear can't trip
// over it), and a traversal-only name can never escape the config directory.
func TestFileKeyringDir(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "keyring")

	t.Setenv("GAFFER_KEYRING_NAME", "")
	if got := fileKeyringDir(dir); got != base {
		t.Errorf("no name: got %q, want %q", got, base)
	}

	t.Setenv("GAFFER_KEYRING_NAME", "vscode")
	if got, want := fileKeyringDir(dir), filepath.Join(dir, "keyring-vscode"); got != want {
		t.Errorf("named: got %q, want %q", got, want)
	}

	// A bare traversal token sanitizes to empty and falls back to the default.
	t.Setenv("GAFFER_KEYRING_NAME", "..")
	if got := fileKeyringDir(dir); got != base {
		t.Errorf("traversal name should fall back to base: got %q, want %q", got, base)
	}

	// Any other name (even a traversal-shaped one, with separators stripped)
	// stays a direct child of the config dir - never nested in the default store,
	// never above the config dir.
	t.Setenv("GAFFER_KEYRING_NAME", "../../etc")
	got := fileKeyringDir(dir)
	if filepath.Dir(got) != dir {
		t.Errorf("sanitized name must stay directly under %q, got %q", dir, got)
	}
	if got == base {
		t.Errorf("a non-empty name must not collide with the default store %q", base)
	}
}

// Regression: a named store must not sit inside the default store's directory,
// or the default store's Clear trips over it ("directory not empty" removing the
// named subdir). Exercises the file backend (no OS keyring in CI).
func TestClearIgnoresNamedSiblingStore(t *testing.T) {
	dir := t.TempDir()
	id := Identity("iss", "cid", "db.example:2113")
	t.Setenv("GAFFER_KEYRING_PASSWORD", "pw")

	// The extension's isolated store holds a token.
	t.Setenv("GAFFER_KEYRING_NAME", "vscode")
	named, err := OpenTokenStore(dir)
	if err != nil {
		t.Fatalf("open named store: %v", err)
	}
	if err := named.Save(id, &oauth2.Token{AccessToken: "v"}); err != nil {
		t.Fatalf("save named: %v", err)
	}

	// The default store clears its own tokens without tripping over the named store.
	t.Setenv("GAFFER_KEYRING_NAME", "")
	def, err := OpenTokenStore(dir)
	if err != nil {
		t.Fatalf("open default store: %v", err)
	}
	if err := def.Save(id, &oauth2.Token{AccessToken: "d"}); err != nil {
		t.Fatalf("save default: %v", err)
	}
	if _, err := def.Clear(); err != nil {
		t.Fatalf("clear default store: %v", err)
	}
}

// DeleteStoredToken removes the token via the non-prompting opener: the
// invalid_grant cleanup path runs on RPC goroutines and must never block on a
// passphrase. TestNonInteractivePassword covers the never-prompt contract of
// the opener it uses; this covers that removal actually lands.
func TestDeleteStoredToken(t *testing.T) {
	dir := t.TempDir()
	id := Identity("iss", "cid", "db.example:2113")

	t.Setenv("GAFFER_KEYRING_PASSWORD", "pw")
	store, err := OpenTokenStore(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := store.Save(id, &oauth2.Token{AccessToken: "a"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := DeleteStoredToken(dir, id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := store.Load(id); !errors.Is(err, ErrNoToken) {
		t.Errorf("token still present after delete: %v", err)
	}
}

func TestTokenStoreRoundTrip(t *testing.T) {
	s := newTokenStore(keyring.NewArrayKeyring(nil))
	id := Identity("https://idp.example.com", "kurrentdb-client", "db.example:2113")

	if _, err := s.Load(id); !errors.Is(err, ErrNoToken) {
		t.Fatalf("expected ErrNoToken before save, got %v", err)
	}

	want := &oauth2.Token{
		AccessToken:  "access",
		RefreshToken: "refresh",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(time.Hour).Round(time.Second),
	}
	if err := s.Save(id, want); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := s.Load(id)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.AccessToken != want.AccessToken || got.RefreshToken != want.RefreshToken || !got.Expiry.Equal(want.Expiry) {
		t.Errorf("round-trip mismatch:\n got %+v\nwant %+v", got, want)
	}
}

func TestTokenStoreDeleteIsIdempotent(t *testing.T) {
	s := newTokenStore(keyring.NewArrayKeyring(nil))
	id := Identity("https://idp.example.com", "c", "db.example:2113")

	if err := s.Delete(id); err != nil {
		t.Fatalf("delete of absent token should be nil, got %v", err)
	}

	if err := s.Save(id, &oauth2.Token{AccessToken: "a"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := s.Delete(id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Load(id); !errors.Is(err, ErrNoToken) {
		t.Fatalf("expected ErrNoToken after delete, got %v", err)
	}
}

func TestTokenStoreClear(t *testing.T) {
	s := newTokenStore(keyring.NewArrayKeyring(nil))

	if n, err := s.Clear(); err != nil || n != 0 {
		t.Fatalf("clear of empty store: got (%d, %v), want (0, nil)", n, err)
	}

	for _, id := range []string{Identity("iss1", "c", "h:2113"), Identity("iss2", "c", "h:2113")} {
		if err := s.Save(id, &oauth2.Token{AccessToken: "a"}); err != nil {
			t.Fatalf("save: %v", err)
		}
	}

	n, err := s.Clear()
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	if n != 2 {
		t.Errorf("cleared %d tokens, want 2", n)
	}
	if _, err := s.Load(Identity("iss1", "c", "h:2113")); !errors.Is(err, ErrNoToken) {
		t.Errorf("expected ErrNoToken after clear, got %v", err)
	}
}

func TestIdentityNormalizesTrailingSlash(t *testing.T) {
	if Identity("https://idp.example.com/", "c", "db.example:2113") != Identity("https://idp.example.com", "c", "db.example:2113") {
		t.Error("issuer trailing slash must not change the identity")
	}
}

func TestIdentityDistinct(t *testing.T) {
	if Identity("iss", "c1", "h") == Identity("iss", "c2", "h") {
		t.Error("different client ids must produce different identities")
	}
	if Identity("iss1", "c", "h") == Identity("iss2", "c", "h") {
		t.Error("different issuers must produce different identities")
	}
	// The UI-1836 property: the same issuer and client bound to a different
	// host is a different identity, so a token never crosses hosts.
	if Identity("iss", "c", "victim.example:2113") == Identity("iss", "c", "attacker.example:2113") {
		t.Error("different hosts must produce different identities")
	}
}

// The never-prompt contract for background callers: the passphrase comes
// from GAFFER_KEYRING_PASSWORD or the access fails closed. filePassword tries
// this first, so a non-tty caller never reaches the terminal prompt.
func TestNonInteractivePassword(t *testing.T) {
	t.Setenv("GAFFER_KEYRING_PASSWORD", "")
	_ = os.Unsetenv("GAFFER_KEYRING_PASSWORD")
	if _, err := nonInteractivePassword("x"); !errors.Is(err, ErrKeyringLocked) {
		t.Fatalf("err = %v, want ErrKeyringLocked with no passphrase source", err)
	}

	t.Setenv("GAFFER_KEYRING_PASSWORD", "pw")
	pw, err := nonInteractivePassword("x")
	if err != nil || pw != "pw" {
		t.Fatalf("got %q, %v; want the env passphrase", pw, err)
	}
}

// filePassword's precedence after the refactor: the env passphrase wins
// without prompting, and with no terminal (go test's stdin is not a tty)
// the file keyring fails closed instead of reaching TerminalPrompt.
func TestFilePassword_NonInteractiveFallback(t *testing.T) {
	t.Setenv("GAFFER_KEYRING_PASSWORD", "")
	_ = os.Unsetenv("GAFFER_KEYRING_PASSWORD")
	if _, err := filePassword(t.TempDir())("x"); !errors.Is(err, ErrKeyringLocked) {
		t.Fatalf("err = %v, want ErrKeyringLocked without a terminal", err)
	}

	t.Setenv("GAFFER_KEYRING_PASSWORD", "pw")
	pw, err := filePassword(t.TempDir())("x")
	if err != nil || pw != "pw" {
		t.Fatalf("got %q, %v; want the env passphrase before any prompt", pw, err)
	}
}
