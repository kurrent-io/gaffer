package cmd

import (
	"strings"
	"testing"
)

func TestEscapeJSString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"it's", `it\'s`},
		{`back\slash`, `back\\slash`},
		{"'); malicious('", `\'); malicious(\'`},
	}

	for _, tt := range tests {
		got := escapeJSString(tt.input)
		if got != tt.expected {
			t.Errorf("escapeJSString(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestGenerateProjectionSource(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		partition string
		emit      bool
		contains  []string
		errMsg    string
	}{
		{
			name:      "fromAll no partition",
			source:    "all",
			partition: "none",
			contains:  []string{"fromAll()", "$init", "when("},
		},
		{
			name:      "fromCategory per-stream",
			source:    "category:orders",
			partition: "per-stream",
			contains:  []string{"fromCategory('orders')", ".foreachStream()"},
		},
		{
			name:      "fromStream",
			source:    "stream:my-stream",
			partition: "none",
			contains:  []string{"fromStream('my-stream')"},
		},
		{
			name:      "with emit",
			source:    "all",
			partition: "none",
			emit:      true,
			contains:  []string{"emit("},
		},
		{
			name:      "invalid partition",
			source:    "all",
			partition: "custom",
			errMsg:    "unsupported partition mode",
		},
		{
			name:      "escapes single quotes in source",
			source:    "stream:it's-a-stream",
			partition: "none",
			contains:  []string{`fromStream('it\'s-a-stream')`},
		},
		{
			name:      "without emit has no emit call",
			source:    "all",
			partition: "none",
			emit:      false,
			contains:  []string{"$init"},
		},
	}

	notContains := map[string][]string{
		"without emit has no emit call": {"emit("},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := generateProjectionSource(tt.source, tt.partition, tt.emit)
			if tt.errMsg != "" {
				if err == nil {
					t.Fatal("expected error")
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Fatalf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatal(err)
			}

			for _, s := range tt.contains {
				if !strings.Contains(result, s) {
					t.Errorf("expected output to contain %q, got:\n%s", s, result)
				}
			}

			for _, s := range notContains[tt.name] {
				if strings.Contains(result, s) {
					t.Errorf("expected output NOT to contain %q, got:\n%s", s, result)
				}
			}
		})
	}
}
