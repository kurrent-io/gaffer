package oauth

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"

	"golang.org/x/oauth2"
)

// successPage is served to the browser once the callback succeeds.
//
//go:embed success.html
var successPage string

// callbackPorts are the loopback ports the login redirect listens on, tried in
// order. They match the redirect URIs registered for the KurrentDB OAuth client
// (and Navigator), so one IdP client configuration serves both.
var callbackPorts = []int{7463, 17463, 27463}

const callbackPath = "/oauth/callback"

// Login runs the OAuth authorization-code flow with PKCE over a loopback
// redirect and returns the resulting token. openBrowser is invoked with the
// authorization URL; the caller is responsible for opening it (and/or printing
// it). The flow ends when the IdP redirects back to the loopback listener, the
// context is cancelled, or no callback port is free.
func Login(ctx context.Context, c Config, openBrowser func(authURL string) error) (*oauth2.Token, error) {
	eps, err := Discover(ctx, c.Issuer)
	if err != nil {
		return nil, err
	}

	listener, port, err := listenLoopback()
	if err != nil {
		return nil, err
	}
	defer func() { _ = listener.Close() }()

	conf := &oauth2.Config{
		ClientID:    c.ClientID,
		Scopes:      c.Scopes,
		Endpoint:    oauth2.Endpoint{AuthURL: eps.AuthorizationEndpoint, TokenURL: eps.TokenEndpoint},
		RedirectURL: fmt.Sprintf("http://127.0.0.1:%d%s", port, callbackPath),
	}

	verifier := oauth2.GenerateVerifier()
	state, err := randomState()
	if err != nil {
		return nil, err
	}

	authOpts := []oauth2.AuthCodeOption{oauth2.AccessTypeOffline, oauth2.S256ChallengeOption(verifier)}
	if c.Audience != "" {
		authOpts = append(authOpts, oauth2.SetAuthURLParam("audience", c.Audience))
	}
	authURL := conf.AuthCodeURL(state, authOpts...)

	type callback struct {
		code string
		err  error
	}
	resultCh := make(chan callback, 1)

	mux := http.NewServeMux()
	mux.HandleFunc(callbackPath, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		send := func(cb callback, msg string, status int) {
			http.Error(w, msg, status)
			select {
			case resultCh <- cb:
			default:
			}
		}
		switch {
		case q.Get("error") != "":
			authErr := q.Get("error")
			if desc := q.Get("error_description"); desc != "" {
				authErr += ": " + desc
			}
			send(callback{err: fmt.Errorf("authorization failed: %s", authErr)}, "Authorization failed. You can close this window.", http.StatusBadRequest)
		case q.Get("state") != state:
			send(callback{err: fmt.Errorf("state mismatch (possible CSRF)")}, "State mismatch. You can close this window.", http.StatusBadRequest)
		case q.Get("code") == "":
			send(callback{err: fmt.Errorf("no authorization code in callback")}, "Missing code. You can close this window.", http.StatusBadRequest)
		default:
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, successPage)
			select {
			case resultCh <- callback{code: q.Get("code")}:
			default:
			}
		}
	})

	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(listener) }()
	defer func() { _ = srv.Close() }()

	if err := openBrowser(authURL); err != nil {
		return nil, fmt.Errorf("open browser: %w", err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-resultCh:
		if res.err != nil {
			return nil, res.err
		}
		tok, err := conf.Exchange(ctx, res.code, oauth2.VerifierOption(verifier))
		if err != nil {
			return nil, fmt.Errorf("exchange code for token: %w", err)
		}
		return tok, nil
	}
}

func listenLoopback() (net.Listener, int, error) {
	var lastErr error
	for _, port := range callbackPorts {
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			return l, port, nil
		}
		lastErr = err
	}
	return nil, 0, fmt.Errorf("no loopback callback port free %v; free one and retry: %w", callbackPorts, lastErr)
}

func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
