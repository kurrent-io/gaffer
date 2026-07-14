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
	sharedTokens.m = map[sourceKey]*tokenEntry{}
	sharedTokens.mu.Unlock()
}

func staticSource() oauth2.TokenSource {
	return oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "x"})
}

// The core of the fix: one source configuration yields one shared source,
// built once even under concurrent resolves, so every consumer of it
// refreshes through a single source instead of racing two.
func TestGetOrBuild_BuildsOncePerKey(t *testing.T) {
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
			s, err := sharedTokens.getOrBuild(sourceKey{identity: "iss|cid"}, build)
			if err != nil {
				t.Errorf("getOrBuild: %v", err)
			}
			got[i] = s
		})
	}
	wg.Wait()

	if builds.Load() != 1 {
		t.Errorf("builds = %d, want 1 for one key", builds.Load())
	}
	for i := range got {
		if got[i] != src {
			t.Errorf("call %d got a different source instance, want the shared one", i)
		}
	}

	// A different setting over the same identity is a different key: the
	// source honours the config it was resolved with instead of silently
	// reusing whichever config resolved first.
	if _, err := sharedTokens.getOrBuild(sourceKey{identity: "iss|cid", caFile: "certs/idp.pem"}, build); err != nil {
		t.Fatal(err)
	}
	if builds.Load() != 2 {
		t.Errorf("builds = %d, want 2 after a second configuration", builds.Load())
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

	key := sourceKey{identity: "id"}
	if _, err := sharedTokens.getOrBuild(key, build); !errors.Is(err, boom) {
		t.Fatalf("first err = %v, want boom", err)
	}
	if _, err := sharedTokens.getOrBuild(key, build); err != nil {
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

	tgt := Target{Env: "prod", OAuth: &config.OAuthConfig{Issuer: "iss", ClientID: "cid"}, AuthHost: "db.example:2113"}
	_, _ = sharedTokens.getOrBuild(tgt.sourceKey(), build)
	_, _ = sharedTokens.getOrBuild(tgt.sourceKey(), build)
	if builds.Load() != 1 {
		t.Fatalf("builds = %d, want 1 before evict", builds.Load())
	}

	EvictTokenSource(tgt)
	_, _ = sharedTokens.getOrBuild(tgt.sourceKey(), build)
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
	tgt := Target{Env: "prod", OAuth: &config.OAuthConfig{Issuer: "iss", ClientID: "cid"}, AuthHost: "db.example:2113"}
	id := tgt.OAuthIdentity()
	if err := store.Save(id, &oauth2.Token{AccessToken: "a", RefreshToken: "r"}); err != nil {
		t.Fatalf("save: %v", err)
	}

	var builds atomic.Int64
	build := func() (oauth2.TokenSource, error) {
		builds.Add(1)
		return staticSource(), nil
	}
	if _, err := sharedTokens.getOrBuild(tgt.sourceKey(), build); err != nil {
		t.Fatal(err)
	}

	InvalidateTokenSource(tgt)

	if _, err := store.Load(id); !errors.Is(err, oauth.ErrNoToken) {
		t.Errorf("stored token still present after invalidate: %v", err)
	}
	if _, err := sharedTokens.getOrBuild(tgt.sourceKey(), build); err != nil {
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

// Sources are memoized by their build inputs: envs with identical OAuth
// settings share one source regardless of env name or project (so refreshes
// serialize), while a differing setting (audience, ca_file, scopes) over the
// same identity gets its own source honouring that config.
func TestSharedTokenSource_KeyedByConfig(t *testing.T) {
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
	if err := store.Save(oauth.Identity(c.Issuer, c.ClientID, "db.example:2113"), &oauth2.Token{
		AccessToken: "tok",
		Expiry:      time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	sA, err := SharedTokenSource(Target{Env: "staging", OAuth: c, AuthHost: "db.example:2113"})
	if err != nil {
		t.Fatalf("SharedTokenSource staging: %v", err)
	}
	sB, err := SharedTokenSource(Target{Env: "prod", OAuth: c, AuthHost: "db.example:2113"})
	if err != nil {
		t.Fatalf("SharedTokenSource prod: %v", err)
	}
	if sA != sB {
		t.Fatal("identical settings must share one source across envs")
	}

	other := &config.OAuthConfig{Issuer: idp.URL, ClientID: "cid", Audience: "kurrentdb"}
	sC, err := SharedTokenSource(Target{Env: "prod", OAuth: other, AuthHost: "db.example:2113"})
	if err != nil {
		t.Fatalf("SharedTokenSource with audience: %v", err)
	}
	if sC == sA {
		t.Fatal("a differing setting over the same identity must not share a source")
	}
}

// Scopes are order-insensitive in the key: the same set spelled in a
// different order is the same configuration.
func TestSourceKey_ScopeOrderInsensitive(t *testing.T) {
	a := Target{OAuth: &config.OAuthConfig{Issuer: "iss", ClientID: "cid", Scopes: []string{"openid", "email"}}, AuthHost: "h:2113"}
	b := Target{OAuth: &config.OAuthConfig{Issuer: "iss", ClientID: "cid", Scopes: []string{"email", "openid"}}, AuthHost: "h:2113"}
	if a.sourceKey() != b.sourceKey() {
		t.Errorf("scope order must not change the key: %+v != %+v", a.sourceKey(), b.sourceKey())
	}
}

// Invalidation purges every cached source over the identity, not only the
// caller's configuration: the stored token they all refresh is deleted, so a
// surviving sibling entry would fail on its dead in-memory chain after the
// user signs back in - and then delete the fresh token.
func TestInvalidateTokenSource_PurgesWholeIdentity(t *testing.T) {
	resetTokenCache()
	dir := t.TempDir()
	t.Setenv("GAFFER_CONFIG_DIR", dir)
	t.Setenv("GAFFER_KEYRING_PASSWORD", "pw")

	c := &config.OAuthConfig{Issuer: "iss", ClientID: "cid"}
	pinned := &config.OAuthConfig{Issuer: "iss", ClientID: "cid", CAFile: "certs/idp.pem"}
	tgt := Target{Env: "prod", OAuth: c, AuthHost: "db.example:2113"}
	sibling := Target{Env: "staging", OAuth: pinned, OAuthCAFile: "certs/idp.pem", AuthHost: "db.example:2113"}

	var builds atomic.Int64
	build := func() (oauth2.TokenSource, error) {
		builds.Add(1)
		return staticSource(), nil
	}
	_, _ = sharedTokens.getOrBuild(tgt.sourceKey(), build)
	_, _ = sharedTokens.getOrBuild(sibling.sourceKey(), build)
	if builds.Load() != 2 {
		t.Fatalf("builds = %d, want 2 distinct configurations", builds.Load())
	}

	InvalidateTokenSource(tgt)

	_, _ = sharedTokens.getOrBuild(tgt.sourceKey(), build)
	_, _ = sharedTokens.getOrBuild(sibling.sourceKey(), build)
	if builds.Load() != 4 {
		t.Errorf("builds = %d, want 4 (both entries purged)", builds.Load())
	}
}

// A target without OAuth has no source or token; the exported cache
// operations no-op instead of dereferencing a nil config.
func TestEvictInvalidate_NoOAuthNoOp(t *testing.T) {
	EvictTokenSource(Target{Env: "basic"})
	InvalidateTokenSource(Target{Env: "basic"})
}
