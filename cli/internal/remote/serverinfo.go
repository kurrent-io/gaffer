package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
)

const (
	// serverInfoStream is the semi-official system stream where a server reports
	// its identity, one $ServerInfo event per update. Populated by an operator
	// (Navigator) or a new-enough DB; absent on most databases today.
	serverInfoStream = "$server-info"
	serverInfoType   = "$ServerInfo"

	// serverInfoScanLimit bounds the backward read for the latest $ServerInfo.
	// A small batch (rather than one event) lets the reader skip an unexpected
	// trailing event type without a re-read, mirroring the definition read.
	serverInfoScanLimit = 10
)

// ServerInfo is a server's self-reported identity from the $server-info stream.
// Both fields are optional on the wire, so Production is tri-state: an explicit
// true/false, or nil when the server didn't report it (including an absent
// stream, where ServerInfo returns nil). Name is "" when unreported.
type ServerInfo struct {
	Name       string
	Production *bool
}

// IsProduction reports whether the server self-identifies as production. Only an
// explicit production=true earns the production guard tier; false, unset, and an
// absent stream (nil receiver) are all baseline. Nil-safe so callers can chain
// off a possibly-nil ServerInfo.
func (s *ServerInfo) IsProduction() bool {
	return s != nil && s.Production != nil && *s.Production
}

// ServerInfo reads the server's self-reported identity from the $server-info
// system stream. It returns nil (no error) when the stream is absent - the
// common case, since most databases don't populate it - so callers get baseline
// behaviour without special-casing. A read or decode failure is returned as an
// error for the caller to weigh.
func (c *Client) ServerInfo(ctx context.Context) (*ServerInfo, error) {
	stream, err := c.db.ReadStream(ctx, serverInfoStream, kurrentdb.ReadStreamOptions{
		Direction: kurrentdb.Backwards,
		From:      kurrentdb.End{},
		// The stream may be projection-emitted/linked, so resolve links to the
		// underlying $ServerInfo. Unlike the definition read this is advisory
		// identity, not leader-critical, so it doesn't require the leader - a
		// follower's copy is fine, and not requiring the leader avoids spurious
		// errors during an election.
		ResolveLinkTos: true,
	}, serverInfoScanLimit)
	if err != nil {
		if errors.Is(classify(err), ErrNotFound) {
			return nil, nil
		}
		return nil, classify(err)
	}
	defer stream.Close()
	return readServerInfo(stream.Recv)
}

// readServerInfo walks events newest-first for the latest $ServerInfo. An absent
// stream or no such event yields nil (no info), not an error. Split from
// ServerInfo so the loop is testable without a live read stream.
func readServerInfo(next func() (*kurrentdb.ResolvedEvent, error)) (*ServerInfo, error) {
	for {
		ev, err := next()
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		if err != nil {
			if errors.Is(classify(err), ErrNotFound) {
				return nil, nil
			}
			return nil, classify(err)
		}
		// Guard a degenerate (nil, nil) and a resolved link with no event.
		if ev == nil || ev.Event == nil || ev.Event.EventType != serverInfoType {
			continue
		}
		return parseServerInfo(ev.Event.Data)
	}
}

func parseServerInfo(data []byte) (*ServerInfo, error) {
	var si struct {
		Name       string `json:"name"`
		Production *bool  `json:"production"`
	}
	if err := json.Unmarshal(data, &si); err != nil {
		return nil, fmt.Errorf("decode server info: %w", err)
	}
	return &ServerInfo{Name: si.Name, Production: si.Production}, nil
}
