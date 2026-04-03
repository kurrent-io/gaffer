package engine

import (
	"encoding/json"
	"fmt"
)

type EventEnvelope struct {
	SequenceNumber int64           `json:"sequenceNumber"`
	StreamID       string          `json:"streamId"`
	EventType      string          `json:"eventType"`
	Data           json.RawMessage `json:"data"`
	Metadata       json.RawMessage `json:"metadata"`
}

func ParseEvent(eventJSON string) EventEnvelope {
	var e EventEnvelope
	_ = json.Unmarshal([]byte(eventJSON), &e)
	return e
}

func (e EventEnvelope) ID() string {
	return fmt.Sprintf("%d@%s", e.SequenceNumber, e.StreamID)
}
