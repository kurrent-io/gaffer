package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/testutil"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// elicitSession wires a real in-memory client+server pair and returns the
// server session, so confirmWrite's Elicit has a live channel to a fake
// human. A nil handler builds a client without the elicitation capability.
func elicitSession(t *testing.T, handler func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error)) *mcp.ServerSession {
	t.Helper()
	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()

	server := mcp.NewServer(&mcp.Implementation{Name: "gate-test", Version: "test"}, nil)
	ss, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })

	opts := &mcp.ClientOptions{}
	if handler != nil {
		opts.ElicitationHandler = handler
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "gate-test-client", Version: "test"}, opts)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return ss
}

func gateReq(ss *mcp.ServerSession) *mcp.CallToolRequest {
	return &mcp.CallToolRequest{Session: ss}
}

func prodGate() writeGate {
	return writeGate{
		Action: `pause projection "orders"`, Name: "orders", Env: "production",
		Target: "orders-prod", Production: true,
		Consequence: "Stops after a final checkpoint; resume with deploy_resume.",
		CLI:         "gaffer disable orders",
	}
}

func TestConfirmWriteNonProdProceeds(t *testing.T) {
	// Not production, not Always: no elicit, no session needed - the client's
	// tool-approval layer is the gate.
	if r := confirmWrite(context.Background(), nil, writeGate{Action: `pause projection "orders"`, Name: "orders", Env: "local"}); r != nil {
		t.Fatalf("non-prod write should proceed without a session, got %v", r)
	}
}

func TestConfirmWriteAccepted(t *testing.T) {
	var asked string
	ss := elicitSession(t, func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		asked = req.Params.Message
		return &mcp.ElicitResult{Action: "accept"}, nil
	})
	if r := confirmWrite(context.Background(), gateReq(ss), prodGate()); r != nil {
		t.Fatalf("accepted confirm should proceed, got %v", r)
	}
	const want = `PRODUCTION [env.production]: pause projection "orders" on "orders-prod"? Stops after a final checkpoint; resume with deploy_resume.`
	if asked != want {
		t.Errorf("question = %q, want %q", asked, want)
	}
}

func TestConfirmWriteDeclined(t *testing.T) {
	// Everything that isn't the exact "accept" token refuses - decline,
	// cancel, and an empty action alike. The allowlist property: a refactor
	// to a decline/cancel denylist would let an unknown action through.
	for _, action := range []string{"decline", "cancel", ""} {
		t.Run("action "+action, func(t *testing.T) {
			ss := elicitSession(t, func(_ context.Context, _ *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
				return &mcp.ElicitResult{Action: action}, nil
			})
			r := confirmWrite(context.Background(), gateReq(ss), prodGate())
			if r == nil || !r.IsError {
				t.Fatalf("%q should refuse, got %v", action, r)
			}
			msg := testutil.MustType[*mcp.TextContent](t, r.Content[0]).Text
			if !strings.Contains(msg, "not confirmed") || !strings.Contains(msg, "nothing was changed") {
				t.Errorf("%q refusal = %q", action, msg)
			}
		})
	}
}

func TestConfirmWriteElicitError(t *testing.T) {
	ss := elicitSession(t, func(_ context.Context, _ *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		return nil, context.DeadlineExceeded
	})
	r := confirmWrite(context.Background(), gateReq(ss), prodGate())
	if r == nil || !r.IsError {
		t.Fatalf("an elicit failure must refuse, got %v", r)
	}
	msg := testutil.MustType[*mcp.TextContent](t, r.Content[0]).Text
	if !strings.Contains(msg, "confirmation failed") || !strings.Contains(msg, "nothing was changed") || !strings.Contains(msg, "gaffer disable orders") {
		t.Errorf("refusal = %q, want the failure, the no-write guarantee, and the CLI next step", msg)
	}
}

func TestWriteGateQuestionUnknownTarget(t *testing.T) {
	// An unreadable $server-info leaves no target name; the question says so
	// plainly instead of quoting an empty string.
	g := writeGate{Action: `pause projection "orders"`, Name: "orders", Env: "prod", Production: true, Consequence: "Stops."}
	g.Target = ""
	const want = `PRODUCTION [env.prod]: pause projection "orders" on the target server? Stops.`
	if got := g.question(); got != want {
		t.Errorf("question = %q, want %q", got, want)
	}
}

func TestConfirmWriteNoElicitationCapability(t *testing.T) {
	ss := elicitSession(t, nil)
	r := confirmWrite(context.Background(), gateReq(ss), prodGate())
	if r == nil || !r.IsError {
		t.Fatalf("a client without elicitation must be refused, got %v", r)
	}
	msg := testutil.MustType[*mcp.TextContent](t, r.Content[0]).Text
	if !strings.Contains(msg, "doesn't support elicitation") || !strings.Contains(msg, "gaffer disable orders") {
		t.Errorf("refusal = %q, want the capability gap and the CLI next step named", msg)
	}
}

func TestConfirmWriteNilSessionFailsClosed(t *testing.T) {
	for name, req := range map[string]*mcp.CallToolRequest{"nil request": nil, "nil session": {}} {
		r := confirmWrite(context.Background(), req, prodGate())
		if r == nil || !r.IsError {
			t.Fatalf("%s: gated write must fail closed, got %v", name, r)
		}
	}
}

func TestConfirmWriteNoUndoElicitsOffProd(t *testing.T) {
	// The no-undo tier off production: still elicits, but as a plain
	// accept/decline - the typed escalation is production-only. The env key
	// trails the target and the consequence follows the question.
	asked := ""
	ss := elicitSession(t, func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		asked = req.Params.Message
		return &mcp.ElicitResult{Action: "accept"}, nil
	})
	g := writeGate{
		Action: `delete projection "orders"`, Name: "orders", Env: "local",
		Target: "staging", NoUndo: true,
		Consequence: "Removes the projection, its state, and checkpoints. No undo.",
		CLI:         "gaffer delete orders",
	}
	if r := confirmWrite(context.Background(), gateReq(ss), g); r != nil {
		t.Fatalf("accepted no-undo gate should proceed, got %v", r)
	}
	const want = `Delete projection "orders" on "staging" [env.local]? Removes the projection, its state, and checkpoints. No undo.`
	if asked != want {
		t.Errorf("question = %q, want %q", asked, want)
	}
	if strings.Contains(asked, "Type the projection name") {
		t.Errorf("off production the no-undo tier must not require typing, got %q", asked)
	}
}

func TestConfirmWriteTypedConfirmOnProd(t *testing.T) {
	// The no-undo tier on production: accept alone isn't enough - the human
	// types the projection name, and a mismatch or missing field refuses.
	g := prodGate()
	g.NoUndo = true

	cases := []struct {
		name    string
		content map[string]any
		wantOK  bool
		wantMsg string
	}{
		{"typed name matches", map[string]any{"confirm": "orders"}, true, ""},
		// The exact-match pattern makes the SDK reject a differing name at
		// schema validation; the gate reports it as the mismatch it is.
		{"typed name differs", map[string]any{"confirm": "order"}, false, "must match"},
		// A missing or mistyped field is rejected by the SDK's own schema
		// validation, which the gate reports with the same must-match
		// message as a pattern failure - accurate for all three.
		{"field missing", nil, false, "must match"},
		{"field wrong type", map[string]any{"confirm": 7}, false, "must match"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var asked string
			var schema []byte
			ss := elicitSession(t, func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
				asked = req.Params.Message
				schema, _ = json.Marshal(req.Params.RequestedSchema)
				return &mcp.ElicitResult{Action: "accept", Content: tc.content}, nil
			})
			r := confirmWrite(context.Background(), gateReq(ss), g)
			// The typing instruction lives on the form field, not the message -
			// clients render the field description at the input, so a message
			// copy doubled up.
			if strings.Contains(asked, "Type the projection name") {
				t.Errorf("question = %q, want the typing instruction only on the field", asked)
			}
			if !strings.Contains(string(schema), `Type the projection name \"orders\" to confirm`) {
				t.Errorf("schema = %s, want the field description carrying the instruction", schema)
			}
			if tc.wantOK {
				if r != nil {
					t.Fatalf("matching typed confirm should proceed, got %v", r)
				}
				return
			}
			if r == nil || !r.IsError {
				t.Fatalf("expected refusal, got %v", r)
			}
			msg := testutil.MustType[*mcp.TextContent](t, r.Content[0]).Text
			if !strings.Contains(msg, tc.wantMsg) || !strings.Contains(msg, "nothing was changed") {
				t.Errorf("refusal = %q", msg)
			}
		})
	}
}

func TestShellQuote(t *testing.T) {
	for in, want := range map[string]string{
		"orders":         "'orders'",
		"weird name":     "'weird name'",
		"a;rm -rf ~":     "'a;rm -rf ~'",
		"it's":           `'it'\''s'`,
		"$(touch pwned)": "'$(touch pwned)'",
		"`touch pwned`":  "'`touch pwned`'",
	} {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %s, want %s", in, got, want)
		}
	}
}
