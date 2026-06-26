package cmd

import (
	"encoding/json"
	"errors"
	"fmt"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// hangGuardHint is appended to local timeout errors so a user knows which lever
// to pull. The [database_config] timeouts are declaration-only, so pointing
// them at the env var heads off the natural-but-wrong fix of raising the config.
var hangGuardHint = "Set " + engine.EnvTimeoutMs + " to raise gaffer's local time limit (the [database_config] timeouts are not applied to local runs)."

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
	WriteRunError(code, description string)
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
	var (
		invalidProj  *gafferruntime.InvalidProjectionError
		handlerErr   *gafferruntime.ProjectionHandlerError
		transformErr *gafferruntime.ProjectionTransformError
		execTimeout  *gafferruntime.ExecutionTimeoutError
		compTimeout  *gafferruntime.CompilationTimeoutError
		malformedEvt *gafferruntime.MalformedEventError
		invalidArg   *gafferruntime.InvalidArgumentError
		stateSerErr  *gafferruntime.StateSerializationError
		projErr      gafferruntime.ProjectionError
	)
	switch {
	case errors.As(err, &invalidProj):
		fe.Code = invalidProj.ErrorCode()
		fe.Description = invalidProj.ErrorDescription()
		fe.CompatCode = invalidProj.CompatCode
		fe.CompatDescription = invalidProj.CompatDescription
		fe.CompatFixedIn = invalidProj.CompatFixedIn
		if invalidProj.Location != nil {
			fe.Line = &invalidProj.Location.Line
			fe.Column = &invalidProj.Location.Column
		}
	case errors.As(err, &handlerErr):
		fe.Code = handlerErr.ErrorCode()
		fe.Description = handlerErr.ErrorDescription()
		fe.JsStack = handlerErr.JsStack
		fe.EventID = formatEventID(handlerErr.Event)
		fe.CompatCode = handlerErr.CompatCode
		fe.CompatDescription = handlerErr.CompatDescription
		fe.CompatFixedIn = handlerErr.CompatFixedIn
		if handlerErr.Location != nil {
			fe.Line = &handlerErr.Location.Line
			fe.Column = &handlerErr.Location.Column
		}
	case errors.As(err, &transformErr):
		fe.Code = transformErr.ErrorCode()
		fe.Description = transformErr.ErrorDescription()
		fe.JsStack = transformErr.JsStack
		fe.CompatCode = transformErr.CompatCode
		fe.CompatDescription = transformErr.CompatDescription
		fe.CompatFixedIn = transformErr.CompatFixedIn
		if transformErr.Location != nil {
			fe.Line = &transformErr.Location.Line
			fe.Column = &transformErr.Location.Column
		}
	case errors.As(err, &execTimeout):
		fe.Code = execTimeout.ErrorCode()
		fe.Description = fmt.Sprintf("%s (elapsed %dms, allowed %dms). %s",
			execTimeout.ErrorDescription(), execTimeout.ElapsedMs, execTimeout.AllowedMs, hangGuardHint)
		fe.EventID = formatEventID(execTimeout.Event)
		fe.CompatCode = execTimeout.CompatCode
		fe.CompatDescription = execTimeout.CompatDescription
		fe.CompatFixedIn = execTimeout.CompatFixedIn
	case errors.As(err, &compTimeout):
		fe.Code = compTimeout.ErrorCode()
		fe.Description = fmt.Sprintf("%s (elapsed %dms, allowed %dms). %s",
			compTimeout.ErrorDescription(), compTimeout.ElapsedMs, compTimeout.AllowedMs, hangGuardHint)
		fe.CompatCode = compTimeout.CompatCode
		fe.CompatDescription = compTimeout.CompatDescription
		fe.CompatFixedIn = compTimeout.CompatFixedIn
	case errors.As(err, &malformedEvt):
		fe.Code = malformedEvt.ErrorCode()
		fe.Description = malformedEvt.ErrorDescription()
		fe.EventID = formatEventID(malformedEvt.Event)
		fe.CompatCode = malformedEvt.CompatCode
		fe.CompatDescription = malformedEvt.CompatDescription
		fe.CompatFixedIn = malformedEvt.CompatFixedIn
	case errors.As(err, &invalidArg):
		fe.Code = invalidArg.ErrorCode()
		fe.Description = invalidArg.ErrorDescription()
		fe.CompatCode = invalidArg.CompatCode
		fe.CompatDescription = invalidArg.CompatDescription
		fe.CompatFixedIn = invalidArg.CompatFixedIn
	case errors.As(err, &stateSerErr):
		fe.Code = stateSerErr.ErrorCode()
		fe.Description = stateSerErr.ErrorDescription()
		fe.EventID = formatEventID(stateSerErr.Event)
		fe.CompatCode = stateSerErr.CompatCode
		fe.CompatDescription = stateSerErr.CompatDescription
		fe.CompatFixedIn = stateSerErr.CompatFixedIn
	case errors.As(err, &projErr):
		fe.Code = projErr.ErrorCode()
		fe.Description = projErr.ErrorDescription()
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

// numberPrinter groups integers with thousands separators. Pinned to English so
// the separator stays a comma regardless of the host locale.
var numberPrinter = message.NewPrinter(language.English)

func formatNumber(n int) string {
	return numberPrinter.Sprintf("%d", n)
}
