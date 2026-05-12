package telemetry

import (
	"context"
	"testing"
)

func TestPeekInvocationFlags_SpaceAndEqualsForms(t *testing.T) {
	args := []string{
		"--invoker-id", "abc-123",
		"--invoked-by=vscode",
		"lsp",
		"--invoked-via", "code_lens",
		"--other-flag=ignore",
	}
	got := PeekInvocationFlags(args)
	want := Invocation{
		InvokerID:  "abc-123",
		InvokedBy:  InvokedByVSCode,
		InvokedVia: InvokedViaCodeLens,
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestPeekInvocationFlags_LaterWins(t *testing.T) {
	args := []string{
		"--invoker-id=first",
		"--invoker-id", "second",
	}
	got := PeekInvocationFlags(args)
	if got.InvokerID != "second" {
		t.Errorf("InvokerID = %q, want %q", got.InvokerID, "second")
	}
}

func TestPeekInvocationFlags_AbsentFlagsLeaveZero(t *testing.T) {
	got := PeekInvocationFlags([]string{"version"})
	if !got.IsZero() {
		t.Errorf("got %+v, want zero Invocation", got)
	}
}

func TestPeekInvocationFlags_TrailingFlagWithoutValue(t *testing.T) {
	// `--invoker-id` at the end with no value should not panic and
	// should not consume args past the end.
	got := PeekInvocationFlags([]string{"--invoker-id"})
	if got.InvokerID != "" {
		t.Errorf("InvokerID = %q, want empty", got.InvokerID)
	}
}

func TestPeekInvocationFlags_BareFormRejectsFlagShapedValue(t *testing.T) {
	// `--invoker-id --invoked-by=x` must NOT set InvokerID to the
	// literal "--invoked-by=x". It must leave InvokerID empty and
	// still consume --invoked-by correctly.
	args := []string{"--invoker-id", "--invoked-by=vscode"}
	got := PeekInvocationFlags(args)
	if got.InvokerID != "" {
		t.Errorf("InvokerID = %q, want empty", got.InvokerID)
	}
	if got.InvokedBy != InvokedByVSCode {
		t.Errorf("InvokedBy = %q, want vscode", got.InvokedBy)
	}
}

func TestPeekInvocationFlags_StopsAtDoubleDash(t *testing.T) {
	// `--` is cobra's end-of-flags marker. Tokens after it are
	// positional and must not be slurped by the peek.
	args := []string{
		"--invoker-id=before",
		"--",
		"--invoker-id=after",
	}
	got := PeekInvocationFlags(args)
	if got.InvokerID != "before" {
		t.Errorf("InvokerID = %q, want before", got.InvokerID)
	}
}

func TestStampInvocation_MCPDefaultsToMCPClientStdio(t *testing.T) {
	// mcp doesn't go through stampInvocation (it's a long-running
	// command, served by stampInvocationBase). Cover the one-shot
	// shape via the dev command's non-mcp default + the long-running
	// path is exercised in TestStampInvocationBase_MCPDefaults.
	c := New(WithSink(newMockSink()), WithIdentity(testIdentity))
	var (
		cmd        CommandName
		dur        RawCount
		invokedBy  InvokedBy
		invokedVia InvokedVia
	)
	c.stampInvocation(&cmd, &dur, &invokedBy, &invokedVia, CommandNameVersion)
	if invokedBy != InvokedByDirect {
		t.Errorf("non-mcp default InvokedBy = %q, want direct", invokedBy)
	}
	if invokedVia != InvokedViaTerminal {
		t.Errorf("non-mcp default InvokedVia = %q, want terminal", invokedVia)
	}
}

func TestStampInvocationBase_MCPDefaults(t *testing.T) {
	c := New(WithSink(newMockSink()), WithIdentity(testIdentity))
	var (
		cmd        CommandName
		dur        RawCount
		outcome    Outcome
		invokedBy  InvokedBy
		invokedVia InvokedVia
	)
	c.stampInvocationBase(&cmd, &dur, &outcome, &invokedBy, &invokedVia, CommandNameMCP, context.Background(), nil)
	if invokedBy != InvokedByMCPClient {
		t.Errorf("mcp default InvokedBy = %q, want mcp_client", invokedBy)
	}
	if invokedVia != InvokedViaStdio {
		t.Errorf("mcp default InvokedVia = %q, want stdio", invokedVia)
	}
}

func TestStampInvocationBase_NonMCPDefaultsToDirectTerminal(t *testing.T) {
	c := New(WithSink(newMockSink()), WithIdentity(testIdentity))
	var (
		cmd        CommandName
		dur        RawCount
		outcome    Outcome
		invokedBy  InvokedBy
		invokedVia InvokedVia
	)
	c.stampInvocationBase(&cmd, &dur, &outcome, &invokedBy, &invokedVia, CommandNameDev, context.Background(), nil)
	if invokedBy != InvokedByDirect {
		t.Errorf("dev default InvokedBy = %q, want direct", invokedBy)
	}
	if invokedVia != InvokedViaTerminal {
		t.Errorf("dev default InvokedVia = %q, want terminal", invokedVia)
	}
}

func TestStampInvocationBase_ExplicitFlagOverridesMCPDefault(t *testing.T) {
	// --invoked-by / --invoked-via take precedence over the
	// command-aware default. Use mcp to prove the non-default path.
	c := New(
		WithSink(newMockSink()),
		WithIdentity(testIdentity),
		WithInvocation(Invocation{
			InvokedBy:  InvokedByVSCode,
			InvokedVia: InvokedViaCommandPalette,
		}),
	)
	var (
		cmd        CommandName
		dur        RawCount
		outcome    Outcome
		invokedBy  InvokedBy
		invokedVia InvokedVia
	)
	c.stampInvocationBase(&cmd, &dur, &outcome, &invokedBy, &invokedVia, CommandNameMCP, context.Background(), nil)
	if invokedBy != InvokedByVSCode {
		t.Errorf("flag-override InvokedBy = %q, want vscode", invokedBy)
	}
	if invokedVia != InvokedViaCommandPalette {
		t.Errorf("flag-override InvokedVia = %q, want command_palette", invokedVia)
	}
}

func TestStampInvocation_FlagOverridesNonMCPDefault(t *testing.T) {
	// One-shot path: `gaffer init --invoked-by=vscode --invoked-via=code_lens`
	// is the real cross-surface case (extension scaffolds a project
	// via a code lens). Cover that stampInvocation honours the
	// Client's invocation state for one-shot commands.
	c := New(
		WithSink(newMockSink()),
		WithIdentity(testIdentity),
		WithInvocation(Invocation{
			InvokedBy:  InvokedByVSCode,
			InvokedVia: InvokedViaCodeLens,
		}),
	)
	var (
		cmd        CommandName
		dur        RawCount
		invokedBy  InvokedBy
		invokedVia InvokedVia
	)
	c.stampInvocation(&cmd, &dur, &invokedBy, &invokedVia, CommandNameInit)
	if invokedBy != InvokedByVSCode {
		t.Errorf("flag-override InvokedBy = %q, want vscode", invokedBy)
	}
	if invokedVia != InvokedViaCodeLens {
		t.Errorf("flag-override InvokedVia = %q, want code_lens", invokedVia)
	}
}

func TestBuildEnvelope_StampsInvokerID(t *testing.T) {
	c := New(
		WithSink(newMockSink()),
		WithIdentity(testIdentity),
		WithInvocation(Invocation{InvokerID: "abc-invoker"}),
	)
	env := c.buildEnvelope(CommandInvoked{Name: "command_invoked", Timestamp: nowTimestamp()})
	if env.Context.InvokerID == nil {
		t.Fatal("Context.InvokerID = nil, want non-nil")
	}
	if *env.Context.InvokerID != "abc-invoker" {
		t.Errorf("Context.InvokerID = %q, want abc-invoker", *env.Context.InvokerID)
	}
}

func TestBuildEnvelope_OmitsInvokerIDWhenUnset(t *testing.T) {
	c := New(WithSink(newMockSink()), WithIdentity(testIdentity))
	env := c.buildEnvelope(CommandInvoked{Name: "command_invoked", Timestamp: nowTimestamp()})
	if env.Context.InvokerID != nil {
		t.Errorf("Context.InvokerID = %q, want nil when unset", *env.Context.InvokerID)
	}
}
