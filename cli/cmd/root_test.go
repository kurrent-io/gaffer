package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// findLeaf resolves the leaf cobra.Command for args under root and
// parses the remaining flags so f.Changed reflects what the user
// typed. Mirrors the lifecycle a real invocation goes through up to
// PersistentPreRunE, which is exactly when emitsStructuredOutput
// is consulted.
func findLeaf(t *testing.T, root *cobra.Command, args []string) *cobra.Command {
	t.Helper()
	leaf, rest, err := root.Find(args)
	if err != nil {
		t.Fatalf("Find(%v): %v", args, err)
	}
	if err := leaf.ParseFlags(rest); err != nil {
		t.Fatalf("ParseFlags(%v): %v", rest, err)
	}
	return leaf
}

// TestEmitsStructuredOutput drives the real root command tree so the
// --json flag check exercises cobra's actual flag inheritance, not a
// hand-rolled cobra.Command stub. A test running against a fresh
// `&cobra.Command{}` would silently pass even if the structured
// commands forgot their annotation.
func TestEmitsStructuredOutput(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"manifest"}, true},
		{[]string{"lsp"}, true},
		{[]string{"mcp"}, true},
		{[]string{"init"}, false},
		{[]string{"scaffold"}, false},
		{[]string{"info", "foo"}, false},
		{[]string{"info", "foo", "--json"}, true},
		{[]string{"dev"}, false},
		{[]string{"dev", "--json"}, true},
		{[]string{"version"}, false},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			leaf := findLeaf(t, NewRootCmd(), tc.args)
			if got := emitsStructuredOutput(leaf); got != tc.want {
				t.Errorf("emitsStructuredOutput(%q) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

// The structured-output annotation is a contract between commands
// and the PreRunE update-check gate. A future command rename that
// drops the annotation must fail loudly here rather than silently
// breaking the notice-suppression for editor-spawned invocations.
func TestStructuredCommands_DeclareAnnotation(t *testing.T) {
	for _, name := range []string{"manifest", "lsp", "mcp"} {
		t.Run(name, func(t *testing.T) {
			leaf := findLeaf(t, NewRootCmd(), []string{name})
			if leaf.Annotations[AnnotationOutput] != OutputStructured {
				t.Errorf("%s missing %s=%s annotation", name, AnnotationOutput, OutputStructured)
			}
		})
	}
}

// A parent-only annotation should cover every runnable child. Today
// none of our structured commands have subcommands, but the walk
// guarantees a single tag on a group node propagates rather than
// requiring each leaf to re-declare.
func TestEmitsStructuredOutput_InheritsFromParent(t *testing.T) {
	parent := &cobra.Command{
		Use:         "structuredgroup",
		Annotations: map[string]string{AnnotationOutput: OutputStructured},
	}
	child := &cobra.Command{Use: "child", Run: func(*cobra.Command, []string) {}}
	parent.AddCommand(child)

	if !emitsStructuredOutput(child) {
		t.Error("child of an annotated parent should be classified as structured")
	}
}
