package subscription

import (
	"encoding/json"
	"time"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
)

func MapEvent(resolved *kurrentdb.ResolvedEvent) (string, error) {
	event := resolved.Event
	if event == nil {
		return "", nil
	}

	isJSON := event.ContentType == "application/json"

	obj := map[string]any{
		"eventType":      event.EventType,
		"streamId":       event.StreamID,
		"sequenceNumber": event.EventNumber,
		"eventId":        event.EventID.String(),
		"created":        event.CreatedDate.UTC().Format(time.RFC3339),
		"isJson":         isJSON,
	}

	if isJSON && len(event.Data) > 0 {
		obj["data"] = json.RawMessage(event.Data)
	} else if !isJSON && len(event.Data) > 0 {
		obj["data"] = string(event.Data)
	} else {
		obj["data"] = nil
	}

	if len(event.UserMetadata) > 0 {
		obj["metadata"] = json.RawMessage(event.UserMetadata)
	}

	if resolved.Link != nil {
		obj["linkMetadata"] = map[string]string{
			"streamId":  resolved.Link.StreamID,
			"eventType": resolved.Link.EventType,
		}
	}

	data, err := json.Marshal(obj)
	if err != nil {
		return "", err
	}

	return string(data), nil
}
