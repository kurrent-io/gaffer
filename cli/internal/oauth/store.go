// Package oauth implements gaffer's OAuth/OIDC authentication: a token store,
// OIDC discovery, the KurrentDB credentials provider, and the interactive
// login flow used by `gaffer auth`.
package oauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/99designs/keyring"
	"golang.org/x/oauth2"
	"golang.org/x/term"
)

const keyringService = "gaffer-oauth"

// ErrNoToken is returned by TokenStore.Load when no token is stored for the
// identity.
var ErrNoToken = errors.New("no stored token")

// ErrKeyringLocked is returned when the encrypted-file keyring needs a
// passphrase but none is available non-interactively (no GAFFER_KEYRING_PASSWORD
// and no terminal to prompt on). Callers treat it as needing a sign-in.
var ErrKeyringLocked = errors.New("keyring is locked and no terminal is available to unlock it")

// TokenStore persists OAuth tokens in the OS keyring (macOS Keychain, Linux
// Secret Service, Windows Credential Manager, ...), falling back to an
// encrypted file when no keyring is available.
type TokenStore struct {
	kr keyring.Keyring
}

// OpenTokenStore opens the token store. The encrypted-file fallback lives under
// dir/keyring; dir is typically the gaffer user-config directory.
func OpenTokenStore(dir string) (*TokenStore, error) {
	keyringDir := filepath.Join(dir, "keyring")
	kr, err := keyring.Open(keyring.Config{
		ServiceName:              keyringService,
		KeychainName:             keyringService,
		KeychainTrustApplication: true,
		FileDir:                  keyringDir,
		FilePasswordFunc:         filePassword(keyringDir),
	})
	if err != nil {
		return nil, fmt.Errorf("open keyring: %w", err)
	}
	return &TokenStore{kr: kr}, nil
}

func newTokenStore(kr keyring.Keyring) *TokenStore { return &TokenStore{kr: kr} }

// Save stores the token for identity, replacing any existing one.
func (s *TokenStore) Save(identity string, tok *oauth2.Token) error {
	data, err := json.Marshal(tok) //nolint:gosec // the token must be serialised to persist it
	if err != nil {
		return fmt.Errorf("encode token: %w", err)
	}
	return s.kr.Set(keyring.Item{
		Key:         identity,
		Data:        data,
		Label:       keyringService,
		Description: "OAuth token",
	})
}

// Load returns the stored token for identity, or ErrNoToken if none is stored.
func (s *TokenStore) Load(identity string) (*oauth2.Token, error) {
	item, err := s.kr.Get(identity)
	if errors.Is(err, keyring.ErrKeyNotFound) {
		return nil, ErrNoToken
	}
	if err != nil {
		return nil, err
	}

	var tok oauth2.Token
	if err := json.Unmarshal(item.Data, &tok); err != nil {
		return nil, fmt.Errorf("decode stored token: %w", err)
	}
	return &tok, nil
}

// Delete removes the stored token for identity. It is a no-op when none exists.
func (s *TokenStore) Delete(identity string) error {
	if err := s.kr.Remove(identity); err != nil && !errors.Is(err, keyring.ErrKeyNotFound) {
		return err
	}
	return nil
}

// Clear removes every token gaffer has stored and returns how many it removed.
// Neither listing nor removal needs the keyring passphrase, so it recovers a
// store whose passphrase has been forgotten.
func (s *TokenStore) Clear() (int, error) {
	keys, err := s.kr.Keys()
	if err != nil {
		return 0, err
	}
	for _, k := range keys {
		if err := s.kr.Remove(k); err != nil && !errors.Is(err, keyring.ErrKeyNotFound) {
			return 0, err
		}
	}
	return len(keys), nil
}

// Identity is the storage key for tokens issued to clientID by issuer. Keying
// on the OAuth identity rather than the env name lets a single login serve
// every project that targets the same issuer and client. The issuer's trailing
// slash is trimmed to match OIDC discovery, so the same issuer spelled with or
// without one resolves to a single stored token.
func Identity(issuer, clientID string) string {
	return strings.TrimRight(issuer, "/") + "|" + clientID
}

// filePassword supplies the passphrase for the encrypted-file fallback. The
// native keyrings ignore it; it only matters on hosts without one (e.g. a
// headless box). It prefers GAFFER_KEYRING_PASSWORD, otherwise prompts.
//
// The keyring backend uses one hook for both first-time creation and later
// unlocks, so its built-in prompt says "unlock" even when setting the
// passphrase. We word it ourselves, keying off whether the store already holds
// any entries, so the first run reads as creating a passphrase rather than
// unlocking one that doesn't exist yet.
func filePassword(keyringDir string) keyring.PromptFunc {
	return func(string) (string, error) {
		if v := os.Getenv("GAFFER_KEYRING_PASSWORD"); v != "" {
			return v, nil
		}
		// A non-interactive caller (an editor, CI, a piped run) has no terminal
		// to prompt on, and TerminalPrompt would write to stdout - corrupting
		// the LSP/DAP protocol stream - before failing opaquely. Fail fast with
		// guidance instead.
		if !term.IsTerminal(int(os.Stdin.Fd())) { //nolint:gosec // term takes an int fd; fds are small, no overflow
			return "", fmt.Errorf("%w: set GAFFER_KEYRING_PASSWORD, or run `gaffer auth` from a terminal", ErrKeyringLocked)
		}
		prompt := "Enter passphrase to unlock gaffer's stored credentials"
		if entries, err := os.ReadDir(keyringDir); err != nil || len(entries) == 0 {
			prompt = "Create a passphrase to protect gaffer's stored credentials"
		}
		return keyring.TerminalPrompt(prompt)
	}
}
