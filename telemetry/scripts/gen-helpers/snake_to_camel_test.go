package main

import "testing"

func TestSnakeToCamel(t *testing.T) {
	for _, tc := range []struct {
		in, want string
	}{
		{"emitter", "Emitter"},
		{"manifest_features_used", "ManifestFeaturesUsed"},
		{"projection_id", "ProjectionID"},
		{"cli_unreachable_reason", "CLIUnreachableReason"},
		{"db_version", "DBVersion"},
		{"duration_ms", "DurationMs"},
		{"runtime_environment", "RuntimeEnvironment"},
		{"lsp_protocol_error", "LSPProtocolError"},
		{"mcp_provider", "MCPProvider"},
		{"dap_protocol_error", "DAPProtocolError"},
		{"ci", "CI"},
		{"url", "URL"},
		{"uri", "URI"},
		{"os", "OS"},
		{"json", "JSON"},
		{"vscode", "VSCode"},
		{"vscodium", "VSCodium"},
		{"ia32", "IA32"},
	} {
		if got := snakeToCamel(tc.in); got != tc.want {
			t.Errorf("snakeToCamel(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
