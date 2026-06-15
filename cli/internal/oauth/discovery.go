package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Endpoints holds the OAuth endpoints discovered from an OIDC issuer.
type Endpoints struct {
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

	resp, err := http.DefaultClient.Do(req)
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
	return eps, nil
}
