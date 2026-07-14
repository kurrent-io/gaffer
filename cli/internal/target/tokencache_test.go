package target

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/oauth"
	"github.com/kurrent-io/gaffer/cli/internal/testutil"
)

// resetTokenCache clears the process-global cache between tests.
func resetTokenCache() {
	sharedTokens.mu.Lock()
	sharedTokens.m = map[string]*tokenEntry{}
	sharedTokens.mu.Unlock()
}

func staticSource() oauth2.TokenSource {
	return oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "x"})
}

// The core of the fix: one identity yields one shared source, built once even
// under concurrent resolves, so the connection and the drift check refresh
// through a single source instead of racing two.
func TestGetOrBuild_BuildsOncePerIdentity(t *testing.T) {
	resetTokenCache()
	var builds atomic.Int64
	src := staticSource()
	build := func() (oauth2.TokenSource, error) {
		builds.Add(1)
		return src, nil
	}

	var wg sync.WaitGroup
	got := make([]oauth2.TokenSource, 8)
	for i := range got {
		wg.Go(func() {
			s, err := sharedTokens.getOrBuild("iss|cid", build)
			if err != nil {
				t.Errorf("getOrBuild: %v", err)
			}
			got[i] = s
		})
	}
	wg.Wait()

	if builds.Load() != 1 {
		t.Errorf("builds = %d, want 1 for one identity", builds.Load())
	}
	for i := range got {
		if got[i] != src {
			t.Errorf("call %d got a different source instance, want the shared one", i)
		}
	}

	if _, err := sharedTokens.getOrBuild("iss|other", build); err != nil {
		t.Fatal(err)
	}
	if builds.Load() != 2 {
		t.Errorf("builds = %d, want 2 after a second identity", builds.Load())
	}
}

// A failed build must not stick: the long-lived MCP server may `gaffer auth`
// between resolves, so the next resolve has to re-read the store.
func TestGetOrBuild_FailureNotCached(t *testing.T) {
	resetTokenCache()
	var builds atomic.Int64
	boom := errors.New("boom")
	build := func() (oauth2.TokenSource, error) {
		if builds.Add(1) == 1 {
			return nil, boom
		}
		return staticSource(), nil
	}

	if _, err := sharedTokens.getOrBuild("id", build); !errors.Is(err, boom) {
		t.Fatalf("first err = %v, want boom", err)
	}
	if _, err := sharedTokens.getOrBuild("id", build); err != nil {
		t.Fatalf("second err = %v, want success after rebuild", err)
	}
	if builds.Load() != 2 {
		t.Errorf("builds = %d, want 2 (a failed build isn't cached)", builds.Load())
	}
}

func TestEvictTokenSource_Rebuilds(t *testing.T) {
	resetTokenCache()
	var builds atomic.Int64
	build := func() (oauth2.TokenSource, error) {
		builds.Add(1)
		return staticSource(), nil
	}

	_, _ = sharedTokens.getOrBuild("id", build)
	_, _ = sharedTokens.getOrBuild("id", build)
	if builds.Load() != 1 {
		t.Fatalf("builds = %d, want 1 before evict", builds.Load())
	}

	EvictTokenSource("id")
	_, _ = sharedTokens.getOrBuild("id", build)
	if builds.Load() != 2 {
		t.Errorf("builds = %d, want 2 after evict", builds.Load())
	}
}

// The engine edge's policy: on invalid_grant the stored token is deleted (dead
// credential, re-sign-in) AND the cache entry evicted (next resolve re-reads).
func TestInvalidateTokenSource_DeletesStoredTokenAndEvicts(t *testing.T) {
	resetTokenCache()
	dir := t.TempDir()
	t.Setenv("GAFFER_CONFIG_DIR", dir)
	t.Setenv("GAFFER_KEYRING_PASSWORD", "pw")
	// Invalidate opens its own store from GAFFER_CONFIG_DIR, so seed the token
	// in the same place.
	store, err := oauth.OpenTokenStore(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	id := oauth.Identity("iss", "cid", "db.example:2113")
	if err := store.Save(id, &oauth2.Token{AccessToken: "a", RefreshToken: "r"}); err != nil {
		t.Fatalf("save: %v", err)
	}

	var builds atomic.Int64
	build := func() (oauth2.TokenSource, error) {
		builds.Add(1)
		return staticSource(), nil
	}
	if _, err := sharedTokens.getOrBuild(id, build); err != nil {
		t.Fatal(err)
	}

	InvalidateTokenSource(id)

	if _, err := store.Load(id); !errors.Is(err, oauth.ErrNoToken) {
		t.Errorf("stored token still present after invalidate: %v", err)
	}
	if _, err := sharedTokens.getOrBuild(id, build); err != nil {
		t.Fatal(err)
	}
	if builds.Load() != 2 {
		t.Errorf("builds = %d, want 2 (entry evicted)", builds.Load())
	}
}

// End to end: two independent resolves of one identity (the connection and the
// drift check) share a single source, so concurrent refreshes collapse to one
// token-endpoint hit. Two separate sources - the bug - would each rotate the
// shared refresh token and hit twice.
func TestSharedTokenSource_SharesOneInstanceAndSerializesRefresh(t *testing.T) {
	resetTokenCache()
	clearCreds(t)
	idp := testutil.NewFakeIDP(t)
	dir := t.TempDir()
	t.Setenv("GAFFER_CONFIG_DIR", dir)
	t.Setenv("GAFFER_KEYRING_PASSWORD", "pw")

	store, err := oauth.OpenTokenStore(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	c := &config.OAuthConfig{Issuer: idp.URL, ClientID: "cid"}
	id := oauth.Identity(c.Issuer, c.ClientID, "db.example:2113")
	// Expired, so the first Token() refreshes; expires_in from the fake keeps
	// the refreshed token valid, so later reads don't re-hit.
	if err := store.Save(id, &oauth2.Token{AccessToken: "old", RefreshToken: "r", Expiry: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatalf("save: %v", err)
	}

	tgt := Target{Env: "prod", OAuth: c, AuthHost: "db.example:2113"}
	s1, err := SharedTokenSource(tgt)
	if err != nil {
		t.Fatalf("SharedTokenSource #1: %v", err)
	}
	s2, err := SharedTokenSource(tgt)
	if err != nil {
		t.Fatalf("SharedTokenSource #2: %v", err)
	}
	if s1 != s2 {
		t.Fatal("two resolves of one identity must share the same source")
	}

	var wg sync.WaitGroup
	for range 6 {
		wg.Go(func() {
			if _, err := s1.Token(); err != nil {
				t.Errorf("Token: %v", err)
			}
		})
	}
	wg.Wait()

	if got := idp.TokenHits.Load(); got != 1 {
		t.Errorf("token endpoint hits = %d, want 1 (one shared, serialized refresh)", got)
	}
}

// The UI-1836 property end to end through the shared cache: a token stored
// for one host satisfies only targets naming that host; a target with the
// same issuer/clientID but another host finds nothing and surfaces the
// sign-in signal instead of the victim's token.
func TestSharedTokenSource_NoCrossHostReuse(t *testing.T) {
	resetTokenCache()
	clearCreds(t)
	idp := testutil.NewFakeIDP(t)
	dir := t.TempDir()
	t.Setenv("GAFFER_CONFIG_DIR", dir)
	t.Setenv("GAFFER_KEYRING_PASSWORD", "pw")

	store, err := oauth.OpenTokenStore(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	c := &config.OAuthConfig{Issuer: idp.URL, ClientID: "cid"}
	if err := store.Save(oauth.Identity(c.Issuer, c.ClientID, "victim.example:2113"), &oauth2.Token{
		AccessToken: "victim-token",
		Expiry:      time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	if _, err := SharedTokenSource(Target{Env: "prod", OAuth: c, AuthHost: "victim.example:2113"}); err != nil {
		t.Fatalf("the authenticated host must resolve: %v", err)
	}

	_, err = SharedTokenSource(Target{Env: "prod", OAuth: c, AuthHost: "attacker.example:2113"})
	var authErr *AuthRequiredError
	if !errors.As(err, &authErr) {
		t.Fatalf("a host the user never authenticated must require sign-in, got %v", err)
	}
}
