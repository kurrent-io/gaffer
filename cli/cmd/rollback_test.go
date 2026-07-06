package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/deploy"
)

func TestRenderRollback(t *testing.T) {
	full := strings.Repeat("ab", 32)
	for _, tc := range []struct {
		jsonOut               bool
		outcome, hash, target string
		want                  string
	}{
		{true, "rolled-back", full, "staging", `{"name":"orders","outcome":"rolled-back","hash":"` + full + `"}` + "\n"},
		{false, "rolled-back", full, "staging", "Rolled back orders to abababa on staging.\n"},
		{false, "rolled-back", full, "", "Rolled back orders to abababa.\n"},
		{false, "unchanged", full, "staging", "orders is already at abababa.\n"},
	} {
		var b bytes.Buffer
		if err := renderRollback(&b, tc.jsonOut, "orders", tc.outcome, tc.hash, tc.target); err != nil {
			t.Fatalf("renderRollback: %v", err)
		}
		if b.String() != tc.want {
			t.Errorf("renderRollback(json=%v, %q) = %q, want %q", tc.jsonOut, tc.outcome, b.String(), tc.want)
		}
	}
}

func TestWriteRollbackPreview(t *testing.T) {
	current := ver(2, "fromAll().when({});\n", true, nil).Definition.Descriptor()
	target := ver(1, "fromStream('a').when({});\n", true, nil).Definition.Descriptor()
	target.Emit = true
	cmp := deploy.Compare(target, current)

	var b bytes.Buffer
	writeRollbackPreview(&b, "orders", current, target, target.Hash(), cmp)
	out := b.String()
	for _, want := range []string{
		shortHash(current.Hash()), shortHash(target.Hash()), // the hash movement
		"emit disabled → enabled", // the scalar flip
		"fromAll", "fromStream",   // both sides of the query diff
		"state built by the newer",  // state caution
		"local files are untouched", // drift caution
	} {
		if !strings.Contains(out, want) {
			t.Errorf("preview missing %q\n%s", want, out)
		}
	}
}
