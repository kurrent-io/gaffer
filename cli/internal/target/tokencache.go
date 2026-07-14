package target

import (
	"sync"

	"golang.org/x/oauth2"

	"github.com/kurrent-io/gaffer/cli/internal/oauth"
	"github.com/kurrent-io/gaffer/cli/internal/userconfig"
)

// tokenCache shares one refreshing token source per resolved env across the
// process. The connection's credentials provider and any background reader
// (the config-drift check) resolve the SAME env and therefore the SAME
// source, so refreshes serialize through its single mutex: the first caller
// rotates the stored refresh token, the next reuses the freshly minted access
// token instead of racing a second rotation.
//
// Two independent sources over one stored refresh token is the bug this
// fixes: with refresh-token rotation / reuse detection, the loser of a
// concurrent refresh is rejected with invalid_grant, and if that loser is the
// connection provider it deletes the token the winner just rotated in and
// forces a spurious re-sign-in.
//
// The sharing unit is one resolved env (sourceKey), NOT the OAuth identity:
// a source is built from per-env config beyond the identity - the ca_file
// that anchors TLS to the IdP - so two envs that happen to name the same
// issuer, client, and host must not silently share whichever config resolved
// first. Each env gets a source honouring its own settings. If such envs are
// used concurrently in one process, their sources can race a refresh over the
// shared stored token and the loser forces a visible re-sign-in - a rare,
// self-healing failure, accepted over a silent trust change.
//
// Only stored-token (interactive) sources are cached. A client-credentials
// grant fetches independently with no refresh token and no shared mutable
// state, so there is nothing to serialize and each caller builds its own.
type tokenCache struct {
	mu sync.Mutex
	m  map[sourceKey]*tokenEntry
}

// sourceKey names one resolved env's token source: the resolution context
// (project root + env name) plus the OAuth identity. Identity in the key
// makes an edited issuer, client, or connection host pick up a fresh source
// on the next resolve in a long-lived process; an edited ca_file alone keeps
// the old source until the process restarts or the entry is evicted.
type sourceKey struct {
	root, env, identity string
}

// sourceKey is the cache key for this target's token source. Only meaningful
// when OAuth is set.
func (t Target) sourceKey() sourceKey {
	return sourceKey{root: t.Root, env: t.Env, identity: t.OAuthIdentity()}
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
// for the resolved env, building it once per sourceKey. The stored token it
// reads stays keyed by oauth.Identity (issuer|clientID|host), so envs naming
// the same host share one sign-in while each env's source honours its own
// OAuth settings. A missing or locked stored token is classified to
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
// connection to manage.
func EvictTokenSource(t Target) {
	sharedTokens.mu.Lock()
	delete(sharedTokens.m, t.sourceKey())
	sharedTokens.mu.Unlock()
}

// InvalidateTokenSource drops the cached source AND deletes the stored token
// for the target. Used at the engine edge on invalid_grant: the credential
// itself is dead (its refresh token was rotated out or reused), so re-sign-in
// is required. The eviction is scoped to this env's source; the stored-token
// delete is by OAuth identity, since that credential is shared by every env
// naming the same host.
//
// The delete goes through oauth.DeleteStoredToken rather than reaching into the
// cached entry: the entry may already be gone (the drift path evicted it first),
// and reading a store off an entry a concurrent rebuild is still populating
// would race. DeleteStoredToken also never prompts - this runs on a live
// command's RPC goroutines, so it must not block on a keyring passphrase. It's
// best-effort anyway: the caller trips re-sign-in, and the next `gaffer auth`
// overwrites the token, so a skipped delete self-heals.
func InvalidateTokenSource(t Target) {
	EvictTokenSource(t)
	dir, err := userconfig.DefaultDir()
	if err != nil {
		return
	}
	_ = oauth.DeleteStoredToken(dir, t.OAuthIdentity())
}
