package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/oauth2"
)

// Endpoints holds the OAuth endpoints discovered from an OIDC issuer.
type Endpoints struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
}

// Discover fetches the issuer's OIDC discovery document
// (`/.well-known/openid-configuration`) and returns its OAuth endpoints.
func Discover(ctx context.Context, issuer string) (Endpoints, error) {
	docURL := strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, docURL, nil)
	if err != nil {
		return Endpoints{}, err
	}

	// Honour an http.Client supplied via the context (oauth2.HTTPClient), so
	// discovery shares the same timeout as token fetches.
	client := http.DefaultClient
	if c, ok := ctx.Value(oauth2.HTTPClient).(*http.Client); ok && c != nil {
		client = c
	}

	resp, err := client.Do(req)
	if err != nil {
		return Endpoints{}, fmt.Errorf("oidc discovery: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Endpoints{}, fmt.Errorf("oidc discovery: %s returned %d", docURL, resp.StatusCode)
	}

	var eps Endpoints
	if err := json.NewDecoder(resp.Body).Decode(&eps); err != nil {
		return Endpoints{}, fmt.Errorf("oidc discovery: decode %s: %w", docURL, err)
	}
	if eps.TokenEndpoint == "" {
		return Endpoints{}, fmt.Errorf("oidc discovery: %s has no token_endpoint", docURL)
	}
	// Mix-up defense (OpenID Connect Discovery 4.3): the document's issuer must
	// match the one we asked for.
	if eps.Issuer != "" && strings.TrimRight(eps.Issuer, "/") != strings.TrimRight(issuer, "/") {
		return Endpoints{}, fmt.Errorf("oidc discovery: document issuer %q does not match %q", eps.Issuer, issuer)
	}
	// The token (and secret) and the authorization code travel to these
	// endpoints, so require TLS (loopback exempt, matching the issuer rule).
	for _, ep := range []string{eps.AuthorizationEndpoint, eps.TokenEndpoint} {
		if ep != "" && !isSecureURL(ep) {
			return Endpoints{}, fmt.Errorf("oidc discovery: endpoint must use https, got %q", ep)
		}
	}
	return eps, nil
}

// isSecureURL reports whether raw is an https URL, or an http URL to a loopback
// host (allowed for local development).
func isSecureURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}
