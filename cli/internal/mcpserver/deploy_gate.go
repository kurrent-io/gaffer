package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// writeGate describes one gated write for confirmWrite: what to ask, where
// it lands, and how loudly.
type writeGate struct {
	// Action is the complete lowercase action phrase with the projection
	// name %q-quoted by the caller: `pause projection "orders"`,
	// `roll projection "orders" back to a1b2c3d`. Each verb supplies the
	// whole phrase - rollback splits its verb around the name, so the gate
	// can't assemble verb+name itself.
	Action string
	// Name is the bare projection name, for the typed confirmation match.
	Name string
	// Env is the resolved environment name ([env.<name>]), always shown so
	// the human sees the thing they actually chose, not just the server's
	// friendly name.
	Env string
	// Target and Production come from client.OperateTarget.
	Target     string
	Production bool
	// NoUndo marks a write with no undo story (delete, recreate, a deploy
	// that rebuilds): it always elicits, and on production the confirm
	// requires typing a value rather than a one-key accept. Off production
	// it stays accept/decline - the escalation is proportional, not
	// harassment.
	NoUndo bool
	// TypedValue and TypedNoun override what the production no-undo confirm
	// asks the human to type: the projection name by default; the deploy
	// tool asks for the environment name, since its plan spans projections.
	TypedValue string
	TypedNoun  string
	// Consequence states what the write does that the human is agreeing to,
	// as a trailing statement after the question ("Stops WITHOUT a final
	// checkpoint; a later resume reprocesses from the last checkpoint
	// written."). Required: every gated write has a consequence.
	Consequence string
	// CLI names the equivalent command for the refusal messages, so an agent
	// blocked by a missing capability can hand the human a next step.
	CLI string
}

// question builds the plain-text single-line confirm message. Production
// front-loads the risk in CAPS with the env key, since the target alone is
// only the server's friendly name and "production" mid-sentence reads as
// part of it; off production the env key trails the target. No markdown -
// clients like Claude Code render elicit messages as plain text.
func (g writeGate) question() string {
	where := `on "` + g.Target + `"`
	if g.Target == "" {
		where = "on the target server"
	}
	if g.Production {
		return fmt.Sprintf("PRODUCTION [env.%s]: %s %s? %s", g.Env, g.Action, where, g.Consequence)
	}
	return fmt.Sprintf("%s %s [env.%s]? %s", capitalizeFirst(g.Action), where, g.Env, g.Consequence)
}

// typedConfirm reports whether this gate requires the human to type the
// projection name: the production escalation of the no-undo tier.
func (g writeGate) typedConfirm() bool {
	return g.NoUndo && g.Production
}

// confirmWrite enforces the human-in-the-loop gate on a write tool. A nil
// result means proceed. Production writes (and no-undo writes) require an
// elicitation confirm - answered by the human through the client's UI, so
// the model can't fabricate it; a no-undo write on production additionally
// requires the human to type the projection name. A client that can't
// elicit gets a refusal pointing at the CLI rather than a model-settable
// escape hatch; a declined or cancelled prompt refuses without writing
// anything.
func confirmWrite(ctx context.Context, req *mcp.CallToolRequest, g writeGate) *mcp.CallToolResult {
	if !g.Production && !g.NoUndo {
		return nil
	}

	// The session carries the elicit channel; without one (a direct handler
	// call, or a transport that never initialized) the gate fails closed.
	if req == nil || req.Session == nil {
		return toolError("%s needs a human confirmation, but this call carries no client session; run `%s` instead", capitalizeFirst(g.Action), g.CLI)
	}
	// Checked upfront (the SDK's Elicit also checks) so a missing capability
	// gets its own message instead of pattern-matching the SDK's bare error
	// string apart from transport failures. A url-only elicitation client
	// can't render the form confirm either.
	if p := req.Session.InitializeParams(); p == nil || p.Capabilities == nil || p.Capabilities.Elicitation == nil ||
		(p.Capabilities.Elicitation.Form == nil && p.Capabilities.Elicitation.URL != nil) {
		return toolError("%s needs a human confirmation, and this MCP client doesn't support elicitation; run `%s` instead", capitalizeFirst(g.Action), g.CLI)
	}

	question := g.question()
	// A bare confirm needs no fields; accept/decline is the answer. The
	// typed tier adds a required string field the human must fill with the
	// projection name. Raw JSON / a plain map rather than jsonschema.Schema:
	// the spec requires the properties member and the struct's omitempty
	// drops an empty map.
	typedValue, typedNoun := g.TypedValue, g.TypedNoun
	if typedValue == "" {
		typedValue = g.Name
	}
	if typedNoun == "" {
		typedNoun = "projection name"
	}
	schema := json.RawMessage(`{"type":"object","properties":{}}`)
	if g.typedConfirm() {
		// The instruction lives on the field alone (clients render the
		// description at the input); repeating it in the message doubled up.
		// The exact-match pattern lets a client gate its Accept inline
		// instead of accept-then-reject. pattern is an extension over the
		// MCP elicitation string-schema subset: a client that enforces it
		// closes the seam, one that ignores unknown keywords loses nothing,
		// and the SDK validates the result against it either way.
		s, err := json.Marshal(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"confirm": map[string]any{
					"type":        "string",
					"pattern":     "^" + regexp.QuoteMeta(typedValue) + "$",
					"description": fmt.Sprintf("Type the %s %q to confirm", typedNoun, typedValue),
				},
			},
			"required": []string{"confirm"},
		})
		if err != nil {
			return toolError("building the confirmation form: %v; nothing was changed", err)
		}
		schema = s
	}

	res, err := req.Session.Elicit(ctx, &mcp.ElicitParams{
		Message:         question,
		RequestedSchema: schema,
	})
	if err != nil {
		// The SDK validates the result against the schema, so a typed name
		// failing the exact-match pattern arrives here as a schema-validation
		// error; report it as the mismatch it is, not transport noise.
		if g.typedConfirm() && strings.Contains(err.Error(), "does not match requested schema") {
			return toolError("%s was not confirmed: the typed %s must match %q exactly; nothing was changed", capitalizeFirst(g.Action), typedNoun, typedValue)
		}
		return toolError("confirmation failed: %v; nothing was changed. Run `%s` to proceed from the CLI", err, g.CLI)
	}
	if res.Action != "accept" {
		action := res.Action
		if action == "" {
			action = "no answer"
		}
		return toolError("%s was not confirmed (%s); nothing was changed", capitalizeFirst(g.Action), action)
	}
	if g.typedConfirm() {
		typed, _ := res.Content["confirm"].(string)
		if typed != typedValue {
			return toolError("%s was not confirmed: the typed %s %q doesn't match %q; nothing was changed", capitalizeFirst(g.Action), typedNoun, typed, typedValue)
		}
	}
	return nil
}

// capitalizeFirst upper-cases the first rune of a lowercase action phrase
// for use at sentence start - the phrase reads lowercase after the
// PRODUCTION prefix's colon and capitalized when it leads the line.
func capitalizeFirst(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}

// shellQuote single-quotes a value for the CLI hand-off strings, so a
// crafted projection name can't smuggle shell syntax into the command a
// human is told to copy and run. POSIX single quotes disable all expansion;
// an embedded single quote closes, escapes, and reopens.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
