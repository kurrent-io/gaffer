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

func TestIsConfigCommand(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{"config telemetry on", []string{"config", "telemetry", "on"}, true},
		{"config telemetry off", []string{"config", "telemetry", "off"}, true},
		{"config telemetry status", []string{"config", "telemetry", "status"}, true},
		{"config telemetry on --quiet", []string{"config", "telemetry", "on", "--quiet"}, true},
		{"config alone", []string{"config"}, true},
		{"with leading flag", []string{"--invoker-id=x", "config", "telemetry", "on"}, true},
		{"invoker-id value equals config", []string{"--invoker-id", "config", "version"}, false},
		{"invoked-by value equals config", []string{"--invoked-by", "config", "dev"}, false},
		{"invoked-via value equals config", []string{"--invoked-via", "config", "dev"}, false},
		{"value-taking flag then config subcmd", []string{"--invoker-id", "abc", "config", "telemetry", "on"}, true},
		{"end-of-flags then config", []string{"--", "config"}, true},
		{"unrelated subcommand", []string{"version"}, false},
		{"dev", []string{"dev", "orders"}, false},
		{"empty", []string{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsConfigCommand(tc.args); got != tc.want {
				t.Errorf("IsConfigCommand(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
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

func TestStampInvocation_NoInvocationLeavesInvokedViaNil(t *testing.T) {
	// One-shot path with no --invoked-via flag: the field stays nil
	// so omitempty drops it from the wire. invoked_by still falls
	// through to the command-aware default (direct for non-mcp).
	c := New(WithSink(newMockSink()), WithIdentity(testIdentity))
	var (
		cmd        CommandName
		dur        RawDuration
		invokedBy  InvokedBy
		invokedVia *InvokedVia
	)
	c.stampInvocation(&cmd, &dur, &invokedBy, &invokedVia, CommandNameVersion)
	if invokedBy != InvokedByDirect {
		t.Errorf("non-mcp default InvokedBy = %q, want direct", invokedBy)
	}
	if invokedVia != nil {
		t.Errorf("InvokedVia = %v, want nil (no flag, no default)", *invokedVia)
	}
}

func TestStampInvocationBase_MCPDefaultsToMCPClient(t *testing.T) {
	// invoked_by has a command-aware default (mcp -> mcp_client).
	// invoked_via no longer does; it stays nil unless a flag was passed.
	c := New(WithSink(newMockSink()), WithIdentity(testIdentity))
	var (
		cmd        CommandName
		dur        RawDuration
		outcome    Outcome
		invokedBy  InvokedBy
		invokedVia *InvokedVia
	)
	c.stampInvocationBase(&cmd, &dur, &outcome, &invokedBy, &invokedVia, CommandNameMCP, context.Background(), nil)
	if invokedBy != InvokedByMCPClient {
		t.Errorf("mcp default InvokedBy = %q, want mcp_client", invokedBy)
	}
	if invokedVia != nil {
		t.Errorf("InvokedVia = %v, want nil (no flag, no default)", *invokedVia)
	}
}

func TestStampInvocationBase_NonMCPDefaultsToDirect(t *testing.T) {
	c := New(WithSink(newMockSink()), WithIdentity(testIdentity))
	var (
		cmd        CommandName
		dur        RawDuration
		outcome    Outcome
		invokedBy  InvokedBy
		invokedVia *InvokedVia
	)
	c.stampInvocationBase(&cmd, &dur, &outcome, &invokedBy, &invokedVia, CommandNameDev, context.Background(), nil)
	if invokedBy != InvokedByDirect {
		t.Errorf("dev default InvokedBy = %q, want direct", invokedBy)
	}
	if invokedVia != nil {
		t.Errorf("InvokedVia = %v, want nil (no flag, no default)", *invokedVia)
	}
}

func TestStampInvocationBase_ExplicitFlagSetsInvokedVia(t *testing.T) {
	// --invoked-by / --invoked-via on the Client's Invocation are
	// the only way invoked_via gets stamped now. Cover both fields.
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
		dur        RawDuration
		outcome    Outcome
		invokedBy  InvokedBy
		invokedVia *InvokedVia
	)
	c.stampInvocationBase(&cmd, &dur, &outcome, &invokedBy, &invokedVia, CommandNameMCP, context.Background(), nil)
	if invokedBy != InvokedByVSCode {
		t.Errorf("flag-override InvokedBy = %q, want vscode", invokedBy)
	}
	if invokedVia == nil || *invokedVia != InvokedViaCommandPalette {
		t.Errorf("InvokedVia = %v, want command_palette", invokedVia)
	}
}

func TestStampInvocation_ExplicitFlagSetsInvokedVia(t *testing.T) {
	// One-shot path: `gaffer init --invoked-by=vscode --invoked-via=code_lens`
	// is the real cross-surface case (extension scaffolds a project
	// via a code lens). Cover that stampInvocation reads
	// c.invocation.InvokedVia onto the output pointer.
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
		dur        RawDuration
		invokedBy  InvokedBy
		invokedVia *InvokedVia
	)
	c.stampInvocation(&cmd, &dur, &invokedBy, &invokedVia, CommandNameInit)
	if invokedBy != InvokedByVSCode {
		t.Errorf("flag-override InvokedBy = %q, want vscode", invokedBy)
	}
	if invokedVia == nil || *invokedVia != InvokedViaCodeLens {
		t.Errorf("InvokedVia = %v, want code_lens", invokedVia)
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

func TestBuildEnvelope_StampsProjectID(t *testing.T) {
	c := New(
		WithSink(newMockSink()),
		WithIdentity(testIdentity),
		WithProjectID("deadbeefdeadbeef"),
	)
	env := c.buildEnvelope(CommandInvoked{Name: "command_invoked", Timestamp: nowTimestamp()})
	if env.Context.ProjectID == nil {
		t.Fatal("Context.ProjectID = nil, want non-nil")
	}
	if *env.Context.ProjectID != "deadbeefdeadbeef" {
		t.Errorf("Context.ProjectID = %q, want deadbeefdeadbeef", *env.Context.ProjectID)
	}
}

func TestBuildEnvelope_OmitsProjectIDWhenUnset(t *testing.T) {
	c := New(WithSink(newMockSink()), WithIdentity(testIdentity))
	env := c.buildEnvelope(CommandInvoked{Name: "command_invoked", Timestamp: nowTimestamp()})
	if env.Context.ProjectID != nil {
		t.Errorf("Context.ProjectID = %q, want nil when unset", *env.Context.ProjectID)
	}
}
