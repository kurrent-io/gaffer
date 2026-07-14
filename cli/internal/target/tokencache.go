package target

import (
	"sync"

	"golang.org/x/oauth2"

	"github.com/kurrent-io/gaffer/cli/internal/oauth"
	"github.com/kurrent-io/gaffer/cli/internal/userconfig"
)

// tokenCache shares one refreshing token source per OAuth identity across the
// process. The connection's credentials provider and any background reader (the
// config-drift check) resolve the SAME source, so refreshes serialize through
// its single mutex: the first caller rotates the stored refresh token, the next
// reuses the freshly minted access token instead of racing a second rotation.
//
// Two independent sources over one stored refresh token is the bug this fixes:
// with refresh-token rotation / reuse detection, the loser of a concurrent
// refresh is rejected with invalid_grant, and if that loser is the connection
// provider it deletes the token the winner just rotated in and forces a
// spurious re-sign-in.
//
// Only stored-token (interactive) sources are cached. A client-credentials
// grant fetches independently with no refresh token and no shared mutable
// state, so there is nothing to serialize and each caller builds its own.
type tokenCache struct {
	mu sync.Mutex
	m  map[string]*tokenEntry
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

var sharedTokens = &tokenCache{m: map[string]*tokenEntry{}}

// SharedTokenSource returns the process-shared, auto-refreshing token source
// for the target's OAuth identity, building it once per oauth.Identity
// (issuer|clientID|host - so envs naming the same host share a source, and a
// token never crosses to another host). A missing or locked stored token is
// classified to *AuthRequiredError (via AsAuthRequired) so every consumer
// surfaces the same sign-in signal. A client-credentials grant (secret set)
// is stateless and built per call rather than cached.
func SharedTokenSource(t Target) (oauth2.TokenSource, error) {
	c := t.OAuth
	if t.OAuthClientSecret != "" {
		src, err := newTokenSource(c, t.OAuthCAFile, t.OAuthClientSecret, t.AuthHost)
		return src, AsAuthRequired(t.Env, err)
	}
	id := t.OAuthIdentity()
	src, err := sharedTokens.getOrBuild(id, func() (oauth2.TokenSource, error) {
		return newTokenSource(c, t.OAuthCAFile, "", t.AuthHost)
	})
	return src, AsAuthRequired(t.Env, err)
}

func (tc *tokenCache) getOrBuild(id string, build func() (oauth2.TokenSource, error)) (oauth2.TokenSource, error) {
	tc.mu.Lock()
	e := tc.m[id]
	if e == nil {
		e = &tokenEntry{}
		tc.m[id] = e
	}
	tc.mu.Unlock()

	e.once.Do(func() { e.src, e.err = build() })
	if e.err != nil {
		// Don't leave a failed build cached: a `gaffer auth` between resolves
		// (the long-lived MCP server) would store a token that a cached
		// ErrNoToken would otherwise hide. Drop this entry so the next resolve
		// rebuilds; a concurrent rebuild that already replaced it is left alone.
		tc.mu.Lock()
		if tc.m[id] == e {
			delete(tc.m, id)
		}
		tc.mu.Unlock()
		return nil, e.err
	}
	return e.src, nil
}

// EvictTokenSource drops the cached source for id so the next resolve rebuilds
// from the store. Used after an invalid_grant on the advisory drift path: the
// in-memory source is dead, but the stored token is left for the connection to
// manage.
func EvictTokenSource(id string) {
	sharedTokens.mu.Lock()
	delete(sharedTokens.m, id)
	sharedTokens.mu.Unlock()
}

// InvalidateTokenSource drops the cached source AND deletes the stored token for
// id. Used at the engine edge on invalid_grant: the credential itself is dead
// (its refresh token was rotated out or reused), so re-sign-in is required.
//
// The delete goes through oauth.DeleteStoredToken rather than reaching into the
// cached entry: the entry may already be gone (the drift path evicted it first),
// and reading a store off an entry a concurrent rebuild is still populating
// would race. DeleteStoredToken also never prompts - this runs on a live
// command's RPC goroutines, so it must not block on a keyring passphrase. It's
// best-effort anyway: the caller trips re-sign-in, and the next `gaffer auth`
// overwrites the token, so a skipped delete self-heals.
func InvalidateTokenSource(id string) {
	EvictTokenSource(id)
	dir, err := userconfig.DefaultDir()
	if err != nil {
		return
	}
	_ = oauth.DeleteStoredToken(dir, id)
}
