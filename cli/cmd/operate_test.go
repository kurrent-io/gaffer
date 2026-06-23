package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/prompt"
)

func TestOpQuestion(t *testing.T) {
	for _, tc := range []struct {
		verb, name, target string
		prod               bool
		want               string
	}{
		{"Stop", "orders", "cluster-x", false, "Stop orders on cluster-x?"},
		{"Delete", "orders", "cluster-x", true, "Delete orders on production cluster-x?"},
		{"Delete", "orders", "", true, "Delete orders on production?"},
		{"Start", "orders", "", false, "Start orders?"},
	} {
		if got := opQuestion(tc.verb, tc.name, tc.target, tc.prod); got != tc.want {
			t.Errorf("opQuestion(%q,%q,%q,%v) = %q, want %q", tc.verb, tc.name, tc.target, tc.prod, got, tc.want)
		}
	}
}

func TestCheckOperable(t *testing.T) {
	if err := checkOperable("order-count"); err != nil {
		t.Errorf("a normal projection should be operable, got %v", err)
	}
	for _, name := range []string{"$by_category", "$streams", "$projections-$all"} {
		if err := checkOperable(name); err == nil || !strings.Contains(err.Error(), "system projection") {
			t.Errorf("%s should be refused as a system projection, got %v", name, err)
		}
	}
}

func TestConfirmOp(t *testing.T) {
	// --yes proceeds without prompting.
	if err := confirmOp("Delete x?", true, false); err != nil {
		t.Errorf("--yes should proceed, got %v", err)
	}
	// --json can't prompt: without --yes it fails closed, with --yes it proceeds.
	if err := confirmOp("Delete x?", false, true); !errors.Is(err, errOperateNeedsConfirm) {
		t.Errorf("--json without --yes should fail closed, got %v", err)
	}
	if err := confirmOp("Delete x?", true, true); err != nil {
		t.Errorf("--json --yes should proceed, got %v", err)
	}
	// Non-interactive (no TTY) without --yes fails closed. Guarded so a TTY run -
	// where this would prompt and block - skips rather than hangs.
	if !prompt.Enabled(false) {
		if err := confirmOp("Delete x?", false, false); !errors.Is(err, errOperateNeedsConfirm) {
			t.Errorf("non-interactive without --yes should fail closed, got %v", err)
		}
	}
}

func TestRenderOperate(t *testing.T) {
	for _, tc := range []struct {
		jsonOut               bool
		name, outcome, target string
		want                  string
	}{
		{true, "orders", "stopped", "cluster-x", `{"name":"orders","outcome":"stopped"}` + "\n"},
		{false, "orders", "stopped", "cluster-x", "Stopped orders on cluster-x.\n"},
		{false, "orders", "deleted", "", "Deleted orders.\n"},
		{false, "orders", "started", "staging", "Started orders on staging.\n"},
	} {
		var b bytes.Buffer
		if err := renderOperate(&b, tc.jsonOut, tc.name, tc.outcome, tc.target); err != nil {
			t.Fatalf("renderOperate: %v", err)
		}
		if b.String() != tc.want {
			t.Errorf("renderOperate(json=%v, %q, %q) = %q, want %q", tc.jsonOut, tc.outcome, tc.target, b.String(), tc.want)
		}
	}
}
