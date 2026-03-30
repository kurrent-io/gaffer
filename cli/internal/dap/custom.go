package dap

import (
	"encoding/json"

	godap "github.com/google/go-dap"
)

// CustomEvent is a DAP event with a custom event type and arbitrary JSON body.
type CustomEvent struct {
	godap.Event
	Body any `json:"body"`
}

// NewCustomEvent creates a custom DAP event.
func NewCustomEvent(event string, body any) *CustomEvent {
	return &CustomEvent{
		Event: NewEvent(event),
		Body:  body,
	}
}

// GafferGotoRequest is a custom DAP request to navigate to a history position.
type GafferGotoRequest struct {
	godap.Request
	Arguments GafferGotoArguments `json:"arguments"`
}

type GafferGotoArguments struct {
	Position int64 `json:"position"`
}

func (r *GafferGotoRequest) GetRequest() *godap.Request { return &r.Request }

// GafferGotoResponse is the response to a gaffer/goto request.
type GafferGotoResponse struct {
	godap.Response
	Body json.RawMessage `json:"body"`
}

// GafferTimelineRequest is a custom DAP request for timeline dot data.
type GafferTimelineRequest struct {
	godap.Request
	Arguments GafferTimelineArguments `json:"arguments"`
}

type GafferTimelineArguments struct {
	From int64 `json:"from"`
	To   int64 `json:"to"`
}

func (r *GafferTimelineRequest) GetRequest() *godap.Request { return &r.Request }

// GafferTimelineResponse is the response to a gaffer/timeline request.
type GafferTimelineResponse struct {
	godap.Response
	Body json.RawMessage `json:"body"`
}

// GafferPartitionStateRequest fetches state for a single partition.
type GafferPartitionStateRequest struct {
	godap.Request
	Arguments GafferPartitionStateArguments `json:"arguments"`
}

type GafferPartitionStateArguments struct {
	Partition string `json:"partition"`
}

func (r *GafferPartitionStateRequest) GetRequest() *godap.Request { return &r.Request }

// GafferPartitionStateResponse is the response to a gaffer/partitionState request.
type GafferPartitionStateResponse struct {
	godap.Response
	Body json.RawMessage `json:"body"`
}

// RegisterCustomRequests registers gaffer custom DAP request types on a codec.
func RegisterCustomRequests(codec *godap.Codec) {
	_ = codec.RegisterRequest("gaffer/goto",
		func() godap.Message { return &GafferGotoRequest{} },
		func() godap.Message { return &GafferGotoResponse{} },
	)
	_ = codec.RegisterRequest("gaffer/timeline",
		func() godap.Message { return &GafferTimelineRequest{} },
		func() godap.Message { return &GafferTimelineResponse{} },
	)
	_ = codec.RegisterRequest("gaffer/partitionState",
		func() godap.Message { return &GafferPartitionStateRequest{} },
		func() godap.Message { return &GafferPartitionStateResponse{} },
	)
}
