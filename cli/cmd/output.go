package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
)

type eventInfo = engine.EventEnvelope

func parseEventInfo(eventJSON string) eventInfo {
	return engine.ParseEvent(eventJSON)
}

type outputWriter interface {
	WriteInfo(proj *engine.Projection, info gafferruntime.ProjectionInfo)
	WriteDebugListening(addr string, port int)
	WriteEvent(event eventInfo)
	WriteResult(eventID string, result *gafferruntime.FeedResult)
	WriteError(eventID string, code string, description string)
	WriteFatalError(fe fatalError)
	WriteAuthRequired(env string)
	WriteSummary(stats engine.EventStats, state engine.StateSummary)
}

// fatalError carries everything the editor / TTY needs to surface a fatal
// projection failure: the runtime error code, a human description, the source
// file the error points at, and (when the runtime can identify it) the JS
// position. eventId is set for handler errors that fail mid-stream. CompatCode
// is set when the throw was driven by an upstream-quirk-compat code path; the
// runtime supplies CompatDescription and CompatFixedIn alongside it so the CLI
// can render a "Compat: <code>... Fixed in KurrentDB X" hint without a lookup.
type fatalError struct {
	Code              string
	Description       string
	File              string
	Line              *int
	Column            *int
	JsStack           string
	EventID           string
	CompatCode        string
	CompatDescription string
	CompatFixedIn     string
}

func toFatalError(err error, sourcePath string) fatalError {
	fe := fatalError{File: sourcePath}
	switch e := err.(type) {
	case *gafferruntime.InvalidProjectionError:
		fe.Code = e.ErrorCode()
		fe.Description = e.ErrorDescription()
		fe.CompatCode = e.CompatCode
		fe.CompatDescription = e.CompatDescription
		fe.CompatFixedIn = e.CompatFixedIn
		if e.Location != nil {
			fe.Line = &e.Location.Line
			fe.Column = &e.Location.Column
		}
	case *gafferruntime.ProjectionHandlerError:
		fe.Code = e.ErrorCode()
		fe.Description = e.ErrorDescription()
		fe.JsStack = e.JsStack
		fe.EventID = formatEventID(e.Event)
		fe.CompatCode = e.CompatCode
		fe.CompatDescription = e.CompatDescription
		fe.CompatFixedIn = e.CompatFixedIn
		if e.Location != nil {
			fe.Line = &e.Location.Line
			fe.Column = &e.Location.Column
		}
	case *gafferruntime.ProjectionTransformError:
		fe.Code = e.ErrorCode()
		fe.Description = e.ErrorDescription()
		fe.JsStack = e.JsStack
		fe.CompatCode = e.CompatCode
		fe.CompatDescription = e.CompatDescription
		fe.CompatFixedIn = e.CompatFixedIn
		if e.Location != nil {
			fe.Line = &e.Location.Line
			fe.Column = &e.Location.Column
		}
	case *gafferruntime.ExecutionTimeoutError:
		fe.Code = e.ErrorCode()
		fe.Description = fmt.Sprintf("%s (elapsed %dms, allowed %dms)",
			e.ErrorDescription(), e.ElapsedMs, e.AllowedMs)
		fe.EventID = formatEventID(e.Event)
		fe.CompatCode = e.CompatCode
		fe.CompatDescription = e.CompatDescription
		fe.CompatFixedIn = e.CompatFixedIn
	case *gafferruntime.CompilationTimeoutError:
		fe.Code = e.ErrorCode()
		fe.Description = fmt.Sprintf("%s (elapsed %dms, allowed %dms)",
			e.ErrorDescription(), e.ElapsedMs, e.AllowedMs)
		fe.CompatCode = e.CompatCode
		fe.CompatDescription = e.CompatDescription
		fe.CompatFixedIn = e.CompatFixedIn
	case *gafferruntime.MalformedEventError:
		fe.Code = e.ErrorCode()
		fe.Description = e.ErrorDescription()
		fe.EventID = formatEventID(e.Event)
		fe.CompatCode = e.CompatCode
		fe.CompatDescription = e.CompatDescription
		fe.CompatFixedIn = e.CompatFixedIn
	case *gafferruntime.InvalidArgumentError:
		fe.Code = e.ErrorCode()
		fe.Description = e.ErrorDescription()
		fe.CompatCode = e.CompatCode
		fe.CompatDescription = e.CompatDescription
		fe.CompatFixedIn = e.CompatFixedIn
	case *gafferruntime.StateSerializationError:
		fe.Code = e.ErrorCode()
		fe.Description = e.ErrorDescription()
		fe.EventID = formatEventID(e.Event)
		fe.CompatCode = e.CompatCode
		fe.CompatDescription = e.CompatDescription
		fe.CompatFixedIn = e.CompatFixedIn
	case gafferruntime.ProjectionError:
		fe.Code = e.ErrorCode()
		fe.Description = e.ErrorDescription()
	default:
		fe.Code = "unexpected-error"
		fe.Description = err.Error()
	}
	if fe.Description == "" {
		fe.Description = err.Error()
	}
	return fe
}

func formatEventID(ec gafferruntime.EventContext) string {
	if ec.StreamID == "" {
		return ""
	}
	return fmt.Sprintf("%d@%s", ec.SequenceNumber, ec.StreamID)
}

type sessionCallbacks interface {
	OnEmit(cb gafferruntime.EmitCallback)
	OnLog(cb gafferruntime.LogCallback)
	OnDiagnostic(cb gafferruntime.DiagnosticCallback)
}

func hasContent(raw json.RawMessage) bool {
	return len(raw) > 0 && string(raw) != "null"
}

func displayJSON(raw json.RawMessage) string {
	if len(raw) > 0 && raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return s
		}
	}
	return string(raw)
}

func formatNumber(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	offset := len(s) % 3
	if offset > 0 {
		b.WriteString(s[:offset])
	}
	for i := offset; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}
