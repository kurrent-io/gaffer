package oauth

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

// ResolveCAFile resolves a configured ca_file path against baseDir (the project
// root). An absolute path or the empty string is returned unchanged.
func ResolveCAFile(caFile, baseDir string) string {
	if caFile == "" || filepath.IsAbs(caFile) {
		return caFile
	}
	return filepath.Join(baseDir, caFile)
}

// WithHTTPClient returns a context carrying an *http.Client that the oauth
// package uses for all of its HTTP - discovery, token fetches, refreshes, and
// the login code exchange. The timeout bounds each request so a slow identity
// provider cannot hang one indefinitely. When caFile is non-empty, the issuer's
// TLS is verified against that PEM CA bundle instead of the system trust store,
// for an IdP served by an internal/self-signed CA.
func WithHTTPClient(ctx context.Context, timeout time.Duration, caFile string) (context.Context, error) {
	transport := &http.Transport{}
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		transport = base.Clone()
	}
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read oauth ca_file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("oauth ca_file %q: no certificates found", caFile)
		}
		if transport.TLSClientConfig == nil {
			transport.TLSClientConfig = &tls.Config{}
		}
		transport.TLSClientConfig.RootCAs = pool
	}
	client := &http.Client{Timeout: timeout, Transport: transport}
	return context.WithValue(ctx, oauth2.HTTPClient, client), nil
}

// Config is the OAuth configuration for an env, independent of the gaffer.toml
// representation.
type Config struct {
	Issuer   string
	ClientID string
	Scopes   []string
	Audience string
}

// TokenSource builds an auto-refreshing token source for the env. A non-empty
// clientSecret selects the client-credentials grant (non-interactive, for CI);
// otherwise the token stored by `gaffer auth` is used and refreshed in place.
func TokenSource(ctx context.Context, c Config, clientSecret string, store *TokenStore) (oauth2.TokenSource, error) {
	eps, err := Discover(ctx, c.Issuer)
	if err != nil {
		return nil, err
	}

	if clientSecret != "" {
		return clientCredentialsSource(ctx, c, clientSecret, eps), nil
	}
	return interactiveSource(ctx, c, store, eps)
}

func clientCredentialsSource(ctx context.Context, c Config, secret string, eps Endpoints) oauth2.TokenSource {
	cc := &clientcredentials.Config{
		ClientID:     c.ClientID,
		ClientSecret: secret,
		TokenURL:     eps.TokenEndpoint,
		Scopes:       c.Scopes,
	}
	if c.Audience != "" {
		cc.EndpointParams = url.Values{"audience": {c.Audience}}
	}
	return cc.TokenSource(ctx)
}

func interactiveSource(ctx context.Context, c Config, store *TokenStore, eps Endpoints) (oauth2.TokenSource, error) {
	if store == nil {
		return nil, errors.New("interactive OAuth requires a token store")
	}
	id := Identity(c.Issuer, c.ClientID)
	tok, err := store.Load(id)
	if err != nil {
		return nil, err // ErrNoToken surfaces as the cause; caller maps to guidance
	}

	conf := &oauth2.Config{
		ClientID: c.ClientID,
		Scopes:   c.Scopes,
		Endpoint: oauth2.Endpoint{AuthURL: eps.AuthorizationEndpoint, TokenURL: eps.TokenEndpoint},
	}

	// conf.TokenSource refreshes via the refresh token when the access token
	// expires; persistingSource writes the rotated token back to the store.
	return &persistingSource{
		base:  conf.TokenSource(ctx, tok),
		store: store,
		id:    id,
		last:  tok,
	}, nil
}

// persistingSource saves a refreshed token back to the store so the rotation
// survives across processes. It is safe for concurrent use (the KurrentDB
// client invokes the provider per RPC).
type persistingSource struct {
	base  oauth2.TokenSource
	store *TokenStore
	id    string

	mu   sync.Mutex
	last *oauth2.Token
}

func (p *persistingSource) Token() (*oauth2.Token, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	tok, err := p.base.Token()
	if err != nil {
		return nil, err
	}
	if p.last == nil || tok.AccessToken != p.last.AccessToken || tok.RefreshToken != p.last.RefreshToken {
		// Best effort: a persistence failure must not break an otherwise
		// valid token. Compare the refresh token too, so a refresh-token
		// rotation with an unchanged access token is still persisted.
		_ = p.store.Save(p.id, tok)
		p.last = tok
	}
	return tok, nil
}
