package oauth

import (
	"errors"
	"testing"
	"time"

	"github.com/99designs/keyring"
	"golang.org/x/oauth2"
)

func TestTokenStoreRoundTrip(t *testing.T) {
	s := newTokenStore(keyring.NewArrayKeyring(nil))
	id := Identity("https://idp.example.com", "kurrentdb-client")

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
	id := Identity("https://idp.example.com", "c")

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

	for _, id := range []string{Identity("iss1", "c"), Identity("iss2", "c")} {
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
	if _, err := s.Load(Identity("iss1", "c")); !errors.Is(err, ErrNoToken) {
		t.Errorf("expected ErrNoToken after clear, got %v", err)
	}
}

func TestIdentityNormalizesTrailingSlash(t *testing.T) {
	if Identity("https://idp.example.com/", "c") != Identity("https://idp.example.com", "c") {
		t.Error("issuer trailing slash must not change the identity")
	}
}

func TestIdentityDistinct(t *testing.T) {
	if Identity("iss", "c1") == Identity("iss", "c2") {
		t.Error("different client ids must produce different identities")
	}
	if Identity("iss1", "c") == Identity("iss2", "c") {
		t.Error("different issuers must produce different identities")
	}
}
