package gafferruntime

import "encoding/json"

// FeedResult holds the result of feeding a single event to a projection.
type FeedResult struct {
	Status      string          `json:"status"`
	SkipReason  string          `json:"reason"`
	Partition   string          `json:"partition"`
	State       json.RawMessage `json:"state"`
	Result      json.RawMessage `json:"result"`
	SharedState json.RawMessage `json:"sharedState"`
	Emitted     []EmittedEvent  `json:"emitted"`
	Logs        []string        `json:"logs"`
	// Diagnostics holds quirks that fired while processing this event (e.g. a
	// biState string slot being JSON-quoted). Runtime, value-dependent, no
	// source range - distinct from compile-time ProjectionInfo.Diagnostics.
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// EmittedEvent represents an event emitted by a projection via emit() or linkTo().
type EmittedEvent struct {
	StreamID  string            `json:"streamId"`
	EventType string            `json:"eventType"`
	Data      *string           `json:"data"`
	IsJson    bool              `json:"isJson"`
	IsLink    bool              `json:"isLink"`
	Metadata  map[string]string `json:"metadata"`
}
