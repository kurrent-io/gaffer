package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// writeGate describes one gated write for confirmWrite: what to ask, where
// it lands, and how loudly.
type writeGate struct {
	// Verb leads the question, imperative and capitalised: "Pause", "Recreate".
	Verb string
	// Name is the projection the write targets.
	Name string
	// Target and Production come from operateTarget.
	Target     string
	Production bool
	// Always elicits regardless of production - for writes with no undo at
	// all (delete), where even a dev environment deserves a human answer.
	Always bool
	// Warning is an optional severity sentence appended to the question
	// ("This deletes the projection and its state; there is no undo.").
	Warning string
	// CLI names the equivalent command for the refusal messages, so an agent
	// blocked by a missing capability can hand the human a next step.
	CLI string
}

// confirmWrite enforces the human-in-the-loop gate on a write tool. A nil
// result means proceed. Production writes (and Always writes) require an
// elicitation confirm - answered by the human through the client's UI, so
// the model can't fabricate it. A client that can't elicit gets a refusal
// pointing at the CLI rather than a model-settable escape hatch; a declined
// or cancelled prompt refuses without writing anything.
func confirmWrite(ctx context.Context, req *mcp.CallToolRequest, g writeGate) *mcp.CallToolResult {
	if !g.Production && !g.Always {
		return nil
	}

	// The session carries the elicit channel; without one (a direct handler
	// call, or a transport that never initialized) the gate fails closed.
	if req == nil || req.Session == nil {
		return toolError("%s %q needs a human confirmation, but this call carries no client session; run `%s` instead", g.Verb, g.Name, g.CLI)
	}
	// Checked upfront (the SDK's Elicit also checks) so a missing capability
	// gets its own message instead of pattern-matching the SDK's bare error
	// string apart from transport failures. A url-only elicitation client
	// can't render the form confirm either.
	if p := req.Session.InitializeParams(); p == nil || p.Capabilities == nil || p.Capabilities.Elicitation == nil ||
		(p.Capabilities.Elicitation.Form == nil && p.Capabilities.Elicitation.URL != nil) {
		return toolError("%s %q on %s needs a human confirmation, and this MCP client doesn't support elicitation; run `%s` instead", g.Verb, g.Name, prodWhere(g.Target, g.Production), g.CLI)
	}

	question := fmt.Sprintf("%s %q on %s?", g.Verb, g.Name, prodWhere(g.Target, g.Production))
	if g.Warning != "" {
		question += " " + g.Warning
	}
	res, err := req.Session.Elicit(ctx, &mcp.ElicitParams{
		Message: question,
		// A bare confirm needs no fields; accept/decline is the answer. Raw
		// JSON rather than a jsonschema.Schema because the spec requires the
		// properties member and the struct's omitempty drops an empty map.
		RequestedSchema: json.RawMessage(`{"type":"object","properties":{}}`),
	})
	if err != nil {
		return toolError("confirmation failed: %v; nothing was changed. Run `%s` to proceed from the CLI", err, g.CLI)
	}
	if res.Action != "accept" {
		action := res.Action
		if action == "" {
			action = "no answer"
		}
		return toolError("%s %q was not confirmed (%s); nothing was changed", g.Verb, g.Name, action)
	}
	return nil
}

// prodWhere names the target for a confirm question: the target with
// "production" prepended when prod, "production" alone when prod with no
// known target, or just the target. Mirrors the CLI's phrasing so the same
// write reads the same everywhere.
func prodWhere(target string, prod bool) string {
	if !prod {
		if target == "" {
			return "the target server"
		}
		return target
	}
	if target == "" {
		return "production"
	}
	return "production " + target
}
