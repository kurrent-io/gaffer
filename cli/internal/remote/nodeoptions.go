package remote

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// NodeProjectionOptions are a node's live projection-engine settings, read from
// its /info/options endpoint for the [database_config] drift check. Fields are
// nil when the node doesn't expose the option (an older server), so a caller
// compares only what's actually reported.
type NodeProjectionOptions struct {
	CompilationTimeoutMs *int64
	ExecutionTimeoutMs   *int64
	MaxStateSizeBytes    *int64
}

// The /info/options names for the [database_config] knobs.
const (
	optCompilationTimeout = "ProjectionCompilationTimeout"
	optExecutionTimeout   = "ProjectionExecutionTimeout"
	optMaxStateSize       = "MaxProjectionStateSize"
)

// nodeOptionsHTTPTimeout bounds the /info/options round-trip independently of
// the caller's context, so an unreachable HTTP surface (firewalled port, no
// HTTP on the node) can't stall an advisory check for the full RPC budget.
const nodeOptionsHTTPTimeout = 3 * time.Second

// FetchNodeOptions reads the target node's projection options over its HTTP
// surface (multiplexed with gRPC on the same port). The endpoint, scheme, and
// basic credentials derive from the connection string; a multi-host string is
// asked via its first host. Advisory by design: callers treat any error -
// unparsable connection, auth refusal, missing endpoint - as "unknown" and
// skip their check, never failing the command.
func FetchNodeOptions(ctx context.Context, connection string) (*NodeProjectionOptions, error) {
	endpoint, user, pass, insecure, err := nodeOptionsEndpoint(connection)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if user != "" {
		req.SetBasicAuth(user, pass)
	}
	client := &http.Client{Timeout: nodeOptionsHTTPTimeout}
	if insecure {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // mirrors the connection string's explicit tlsVerifyCert=false
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck // read-only response
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("options endpoint returned %s", resp.Status)
	}
	return parseNodeOptions(resp.Body)
}

// nodeOptionsEndpoint derives the /info/options URL and credentials from a
// KurrentDB connection string: tls=false selects http (the default is TLS),
// tlsVerifyCert=false skips verification, and the userinfo carries the basic
// credentials. A multi-host list is asked via its first host; a host without a
// port gets the default 2113.
func nodeOptionsEndpoint(connection string) (endpoint, user, pass string, insecure bool, err error) {
	u, err := url.Parse(connection)
	if err != nil {
		// The connection string can carry credentials (url.Error embeds the
		// URL), so the error is described, never wrapped.
		return "", "", "", false, fmt.Errorf("unparsable connection string")
	}
	host, _, _ := strings.Cut(u.Host, ",")
	if host == "" {
		return "", "", "", false, fmt.Errorf("connection string has no host")
	}
	if _, _, err := net.SplitHostPort(host); err != nil {
		// JoinHostPort re-brackets a host containing colons, so a bracketed
		// IPv6 literal is unwrapped first rather than double-bracketed.
		host = net.JoinHostPort(strings.Trim(host, "[]"), "2113")
	}

	q := u.Query()
	scheme := "https"
	if strings.EqualFold(q.Get("tls"), "false") {
		scheme = "http"
	}
	insecure = strings.EqualFold(q.Get("tlsVerifyCert"), "false")

	if u.User != nil {
		user = u.User.Username()
		pass, _ = u.User.Password()
	}
	return scheme + "://" + host + "/info/options", user, pass, insecure, nil
}

// parseNodeOptions extracts the projection knobs from the /info/options
// payload: an array of {name, value} objects with stringified values.
func parseNodeOptions(r io.Reader) (*NodeProjectionOptions, error) {
	var raw []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, err
	}
	node := &NodeProjectionOptions{}
	for _, o := range raw {
		v, err := strconv.ParseInt(o.Value, 10, 64)
		if err != nil {
			continue // a knob we want is numeric; anything else isn't ours
		}
		switch o.Name {
		case optCompilationTimeout:
			node.CompilationTimeoutMs = &v
		case optExecutionTimeout:
			node.ExecutionTimeoutMs = &v
		case optMaxStateSize:
			node.MaxStateSizeBytes = &v
		}
	}
	return node, nil
}
