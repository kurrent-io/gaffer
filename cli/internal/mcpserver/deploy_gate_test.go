package mcpserver

import (
	"context"
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
	return writeGate{Verb: "Pause", Name: "orders", Target: "orders-prod", Production: true, CLI: "gaffer disable orders"}
}

func TestConfirmWriteNonProdProceeds(t *testing.T) {
	// Not production, not Always: no elicit, no session needed - the client's
	// tool-approval layer is the gate.
	if r := confirmWrite(context.Background(), nil, writeGate{Verb: "Pause", Name: "orders"}); r != nil {
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
	if !strings.Contains(asked, `Pause "orders" on production orders-prod?`) {
		t.Errorf("question = %q, want the verb, projection, and production target named", asked)
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

func TestProdWhere(t *testing.T) {
	for _, tc := range []struct {
		target string
		prod   bool
		want   string
	}{
		{"orders-prod", true, "production orders-prod"},
		{"", true, "production"},
		{"staging", false, "staging"},
		{"", false, "the target server"},
	} {
		if got := prodWhere(tc.target, tc.prod); got != tc.want {
			t.Errorf("prodWhere(%q, %v) = %q, want %q", tc.target, tc.prod, got, tc.want)
		}
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

func TestConfirmWriteAlwaysElicitsOffProd(t *testing.T) {
	// Delete's tier: Always elicits even off production, and the warning
	// rides along in the question.
	asked := ""
	ss := elicitSession(t, func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		asked = req.Params.Message
		return &mcp.ElicitResult{Action: "accept"}, nil
	})
	g := writeGate{Verb: "Delete", Name: "orders", Target: "staging", Always: true, Warning: "There is no undo.", CLI: "gaffer delete orders"}
	if r := confirmWrite(context.Background(), gateReq(ss), g); r != nil {
		t.Fatalf("accepted always-gate should proceed, got %v", r)
	}
	if !strings.Contains(asked, `Delete "orders" on staging?`) || !strings.Contains(asked, "There is no undo.") {
		t.Errorf("question = %q, want the non-prod target and the warning", asked)
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
