package remote

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/kurrent-io/gaffer/cli/internal/target"
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
// surface (multiplexed with gRPC on the same port). The endpoint and scheme
// derive from the target's connection string; a multi-host string is asked
// via its first host. Authentication follows the target, in the connection's
// own precedence: an OAuth bearer token when the env uses OAuth, else the
// target's resolved basic credentials (which win over the connection
// string's userinfo - UI-1820), else the userinfo; a user certificate is
// presented in the TLS handshake, and the connection's tlsCaFile /
// tlsVerifyCert settings are honoured. Advisory by design: callers surface
// errors as warnings, never failing the command.
func FetchNodeOptions(ctx context.Context, tgt target.Target) (*NodeProjectionOptions, error) {
	endpoint, user, pass, tlsOpts, err := nodeOptionsEndpoint(tgt.Connection)
	if err != nil {
		return nil, err
	}
	if tgt.Username != "" {
		user, pass = tgt.Username, tgt.Password
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	switch {
	case tgt.BearerToken != nil:
		tok, err := tgt.BearerToken(ctx)
		if err != nil {
			return nil, fmt.Errorf("resolving bearer token: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+tok)
	case user != "":
		req.SetBasicAuth(user, pass)
	}
	client := &http.Client{
		Timeout: nodeOptionsHTTPTimeout,
		// The read targets exactly one endpoint it derived itself; a redirect
		// means a misbehaving (or malicious) node, and following one would
		// hand it a blind SSRF primitive. Refuse.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("options endpoint redirected")
		},
	}
	tlsCfg, err := nodeOptionsTLS(tlsOpts, tgt)
	if err != nil {
		return nil, err
	}
	if tlsCfg != nil {
		client.Transport = &http.Transport{TLSClientConfig: tlsCfg}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck // read-only response
	if resp.StatusCode != http.StatusOK {
		// StatusCode, never resp.Status: the reason phrase is server-chosen
		// text, and this error reaches terminals and MCP tool results.
		return nil, fmt.Errorf("options endpoint returned status %d", resp.StatusCode)
	}
	return parseNodeOptions(resp.Body)
}

// nodeOptionsTLS builds the TLS config for the read, or nil when the default
// suffices: skip-verify and CA from the connection string's own settings, and
// the target's user certificate presented in the handshake like the gRPC
// connection presents it.
func nodeOptionsTLS(opts nodeTLSOptions, tgt target.Target) (*tls.Config, error) {
	if !opts.insecure && opts.caFile == "" && tgt.CertFile == "" {
		return nil, nil
	}
	cfg := &tls.Config{InsecureSkipVerify: opts.insecure} //nolint:gosec // mirrors the connection string's explicit tlsVerifyCert=false
	if opts.caFile != "" {
		pem, err := os.ReadFile(opts.caFile)
		if err != nil {
			return nil, fmt.Errorf("reading tlsCaFile: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("tlsCaFile contains no usable certificates")
		}
		cfg.RootCAs = pool
	}
	if tgt.CertFile != "" {
		cert, err := tls.LoadX509KeyPair(tgt.CertFile, tgt.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("loading user certificate: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	return cfg, nil
}

// nodeTLSOptions are the TLS settings a connection string carries for the
// HTTP read: tlsVerifyCert=false and tlsCaFile, mirrored from what the gRPC
// client honours.
type nodeTLSOptions struct {
	insecure bool
	caFile   string
}

// nodeOptionsEndpoint derives the /info/options URL, credentials, and TLS
// settings from a KurrentDB connection string: tls=false selects http (the
// default is TLS), tlsVerifyCert=false skips verification, tlsCaFile names
// the verification CA, and the userinfo carries the basic credentials. A
// multi-host list is asked via its first host; a host without a port gets
// the default 2113.
//
// This is deliberately a second parser beside kurrentdb.ParseConnectionString
// (which can't be reused wholesale: it rejects bracketed IPv6 literals and
// routes multi-host strings through a different URL shape). The boolean and
// key dialect is matched to the client's via queryFlag/queryValue so the two
// parsers can't disagree about what a string means; anyone adding a TLS knob
// here must mirror the client's reading of it.
func nodeOptionsEndpoint(connection string) (endpoint, user, pass string, tlsOpts nodeTLSOptions, err error) {
	u, err := url.Parse(firstHostConnection(connection))
	if err != nil {
		// The connection string can carry credentials (url.Error embeds the
		// URL), so the error is described, never wrapped.
		return "", "", "", nodeTLSOptions{}, errors.New("unparsable connection string")
	}
	host := u.Host
	if host == "" {
		return "", "", "", nodeTLSOptions{}, errors.New("connection string has no host")
	}
	if _, _, err := net.SplitHostPort(host); err != nil {
		// JoinHostPort re-brackets a host containing colons, so a bracketed
		// IPv6 literal is unwrapped first rather than double-bracketed.
		host = net.JoinHostPort(strings.Trim(host, "[]"), "2113")
	}

	q := u.Query()
	scheme := "https"
	if v, ok := queryFlag(q, "tls"); ok && !v {
		scheme = "http"
	}
	if v, ok := queryFlag(q, "tlsVerifyCert"); ok && !v {
		tlsOpts.insecure = true
	}
	tlsOpts.caFile = queryValue(q, "tlsCaFile")

	if u.User != nil {
		user = u.User.Username()
		pass, _ = u.User.Password()
	}
	return scheme + "://" + host + "/info/options", user, pass, tlsOpts, nil
}

// queryFlag reads a boolean connection-string setting the way the kurrentdb
// client's parser does - key matched case-insensitively, value through
// strconv.ParseBool of the lowercased text (so tls=0, tls=F, TLS=FALSE all
// count) - because the HTTP read and the gRPC dial must not disagree about
// what a connection string means. An unparsable value reads as unset; the
// dial would have refused the string before this advisory read ran.
func queryFlag(q url.Values, name string) (value, ok bool) {
	v := queryValue(q, name)
	if v == "" {
		return false, false
	}
	b, err := strconv.ParseBool(strings.ToLower(v))
	if err != nil {
		return false, false
	}
	return b, true
}

// queryValue reads a connection-string setting with the client parser's
// case-insensitive key matching.
func queryValue(q url.Values, name string) string {
	for k, vs := range q {
		if strings.EqualFold(k, name) && len(vs) > 0 {
			return vs[0]
		}
	}
	return ""
}

// firstHostConnection reduces a multi-host connection string to its first host
// before URL parsing: url.Parse's tolerance of a comma-separated authority
// varies by Go version, so the cut happens at the string level - from the
// first comma up to the path or query.
func firstHostConnection(connection string) string {
	comma := strings.Index(connection, ",")
	if comma < 0 {
		return connection
	}
	rest := connection[comma:]
	if end := strings.IndexAny(rest, "/?"); end >= 0 {
		return connection[:comma] + rest[end:]
	}
	return connection[:comma]
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
