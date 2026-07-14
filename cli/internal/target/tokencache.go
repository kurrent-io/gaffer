package target

import (
	"slices"
	"strings"
	"sync"

	"golang.org/x/oauth2"

	"github.com/kurrent-io/gaffer/cli/internal/oauth"
	"github.com/kurrent-io/gaffer/cli/internal/userconfig"
)

// tokenCache shares one refreshing token source per source configuration
// across the process. Every consumer whose target resolves the same build
// inputs - the connection's credentials provider, the config-drift check, any
// other env or project with an identical OAuth block - gets the SAME source,
// so refreshes serialize through its single mutex: the first caller rotates
// the stored refresh token, the next reuses the freshly minted access token
// instead of racing a second rotation.
//
// Two independent sources over one stored refresh token is the bug this
// fixes: with refresh-token rotation / reuse detection, the loser of a
// concurrent refresh is rejected with invalid_grant, and if that loser is the
// connection provider it deletes the token the winner just rotated in and
// forces a spurious re-sign-in.
//
// The cache memoizes newTokenSource by its inputs (sourceKey), not by the
// OAuth identity alone: a source is also shaped by the env's ca_file (the TLS
// anchor for discovery and refresh), scopes, and audience, so two envs that
// name the same issuer, client, and host but declare different settings get
// separate sources, each honouring its own config. Such envs share one stored
// token, so their separate sources can fork its rotation chain; the loser's
// invalid_grant then triggers re-sign-in. Envs with identical settings share
// one source and cannot fork. Keying by inputs also means an edited setting
// resolves to a fresh source on the next dial instead of serving the old
// config until the process restarts.
//
// Only stored-token (interactive) sources are cached. A client-credentials
// grant fetches independently with no refresh token and no shared mutable
// state, so there is nothing to serialize and each caller builds its own.
type tokenCache struct {
	mu sync.Mutex
	m  map[sourceKey]*tokenEntry
}

// sourceKey is the full set of inputs newTokenSource is built from: the OAuth
// identity (issuer|clientID|host) plus the per-env settings that shape the
// source. Scopes are order-insensitive in the key.
type sourceKey struct {
	identity, caFile, audience, scopes string
}

// sourceKey is the cache key for this target's token source. Only meaningful
// when OAuth is set.
func (t Target) sourceKey() sourceKey {
	return sourceKey{
		identity: t.OAuthIdentity(),
		caFile:   t.OAuthCAFile,
		audience: t.OAuth.Audience,
		scopes:   strings.Join(slices.Sorted(slices.Values(t.OAuth.Scopes)), "\x00"),
	}
}

// tokenEntry memoizes one build. once ensures a single build per entry even
// under concurrent resolves; a failed build is evicted (not cached) so a later
// resolve re-reads the store after a mid-process `gaffer auth`. Only src and err
// are read across goroutines, and only after once.Do, which carries the
// happens-before - so the entry needs no other synchronisation.
type tokenEntry struct {
	once sync.Once
	src  oauth2.TokenSource
	err  error
}

var sharedTokens = &tokenCache{m: map[sourceKey]*tokenEntry{}}

// SharedTokenSource returns the process-shared, auto-refreshing token source
// for the resolved target, building it once per sourceKey. The stored token it
// reads stays keyed by oauth.Identity (issuer|clientID|host), so envs naming
// the same host share one sign-in while each source honours the settings it
// was resolved with. A missing or locked stored token is classified to
// *AuthRequiredError (via AsAuthRequired) so every consumer surfaces the same
// sign-in signal. A client-credentials grant (secret set) is stateless and
// built per call rather than cached.
func SharedTokenSource(t Target) (oauth2.TokenSource, error) {
	c := t.OAuth
	if t.OAuthClientSecret != "" {
		src, err := newTokenSource(c, t.OAuthCAFile, t.OAuthClientSecret, t.AuthHost)
		return src, AsAuthRequired(t.Env, err)
	}
	src, err := sharedTokens.getOrBuild(t.sourceKey(), func() (oauth2.TokenSource, error) {
		return newTokenSource(c, t.OAuthCAFile, "", t.AuthHost)
	})
	return src, AsAuthRequired(t.Env, err)
}

func (tc *tokenCache) getOrBuild(key sourceKey, build func() (oauth2.TokenSource, error)) (oauth2.TokenSource, error) {
	tc.mu.Lock()
	e := tc.m[key]
	if e == nil {
		e = &tokenEntry{}
		tc.m[key] = e
	}
	tc.mu.Unlock()

	e.once.Do(func() { e.src, e.err = build() })
	if e.err != nil {
		// Don't leave a failed build cached: a `gaffer auth` between resolves
		// (the long-lived MCP server) would store a token that a cached
		// ErrNoToken would otherwise hide. Drop this entry so the next resolve
		// rebuilds; a concurrent rebuild that already replaced it is left alone.
		tc.mu.Lock()
		if tc.m[key] == e {
			delete(tc.m, key)
		}
		tc.mu.Unlock()
		return nil, e.err
	}
	return e.src, nil
}

// EvictTokenSource drops the cached source for the target so the next resolve
// rebuilds from the store. Used after an invalid_grant on the advisory drift
// path: the in-memory source is dead, but the stored token is left for the
// connection to manage. A target without OAuth has no source; no-op.
func EvictTokenSource(t Target) {
	if t.OAuth == nil {
		return
	}
	sharedTokens.mu.Lock()
	delete(sharedTokens.m, t.sourceKey())
	sharedTokens.mu.Unlock()
}

// InvalidateTokenSource drops every cached source over the target's OAuth
// identity AND deletes its stored token. Used at the engine edge on
// invalid_grant: the credential itself is dead (its refresh token was rotated
// out or reused), so re-sign-in is required. Eviction covers the whole
// identity, not just the caller's key: other configurations' sources refresh
// the same stored credential, and one left cached would fail on its dead
// in-memory chain after the user signs back in - and then delete the fresh
// token when its own invalid_grant lands. A target without OAuth has no
// source or token; no-op.
//
// The delete goes through oauth.DeleteStoredToken rather than reaching into the
// cached entries: an entry may already be gone (the drift path evicted it
// first), and reading a store off an entry a concurrent rebuild is still
// populating would race. DeleteStoredToken also never prompts - this runs on a
// live command's RPC goroutines, so it must not block on a keyring passphrase.
// It's best-effort anyway: the caller trips re-sign-in, and the next
// `gaffer auth` overwrites the token, so a skipped delete self-heals.
func InvalidateTokenSource(t Target) {
	if t.OAuth == nil {
		return
	}
	id := t.OAuthIdentity()
	sharedTokens.mu.Lock()
	for k := range sharedTokens.m {
		if k.identity == id {
			delete(sharedTokens.m, k)
		}
	}
	sharedTokens.mu.Unlock()

	dir, err := userconfig.DefaultDir()
	if err != nil {
		return
	}
	_ = oauth.DeleteStoredToken(dir, id)
}
