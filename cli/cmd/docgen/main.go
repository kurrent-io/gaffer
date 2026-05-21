// docgen generates a single Markdown reference page covering every
// visible gaffer subcommand, for inclusion in the user-facing docs.
//
//	go run ./cmd/docgen <output-file>
//
// Hidden commands and flags are skipped. The root `gaffer` command is
// not emitted; its short description is assumed to live in the page's
// frontmatter or intro.
package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kurrent-io/gaffer/cli/cmd"
)

// topLevelOrder is the precedence used for sorting root subcommands so
// the page reads in the order a new user would meet the commands:
// init, scaffold, dev, then runtime helpers, then config and version.
// Unknown commands sort after every listed entry, alphabetically.
var topLevelOrder = []string{
	"init",
	"scaffold",
	"dev",
	"info",
	"mcp",
	"lsp",
	"config",
	"version",
}

const frontmatter = `---
title: Commands
description: Full reference for every gaffer subcommand and its flags.
order: 2
---

Full reference for every gaffer subcommand. Generated from the CLI source; run ` + "`just gen-docs`" + ` to refresh after touching a command.

`

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "usage: %s <output-file>\n", os.Args[0])
		os.Exit(2)
	}
	out := os.Args[1]

	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	var buf bytes.Buffer
	buf.WriteString(frontmatter)

	root := cmd.NewRootCmd()
	for _, sub := range visibleCommands(root) {
		writeCommand(&buf, sub)
	}

	if err := os.WriteFile(out, buf.Bytes(), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// visibleCommands returns every descendant of root that should be
// documented, in pre-order traversal. The root command itself is
// omitted; hidden commands and any descendants are skipped. Direct
// children of root are sorted by topLevelOrder so the page reads in
// onboarding order; nested subcommands keep cobra's default
// alphabetical order.
func visibleCommands(root *cobra.Command) []*cobra.Command {
	var out []*cobra.Command
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			if sub.Hidden || !sub.IsAvailableCommand() {
				continue
			}
			out = append(out, sub)
			walk(sub)
		}
	}
	for _, sub := range sortTopLevel(root.Commands()) {
		if sub.Hidden || !sub.IsAvailableCommand() {
			continue
		}
		out = append(out, sub)
		walk(sub)
	}
	return out
}

func sortTopLevel(in []*cobra.Command) []*cobra.Command {
	out := slices.Clone(in)
	slices.SortStableFunc(out, func(a, b *cobra.Command) int {
		ai, bi := slices.Index(topLevelOrder, a.Name()), slices.Index(topLevelOrder, b.Name())
		switch {
		case ai >= 0 && bi >= 0:
			return ai - bi
		case ai >= 0:
			return -1
		case bi >= 0:
			return 1
		default:
			return strings.Compare(a.Name(), b.Name())
		}
	})
	return out
}

func writeCommand(buf *bytes.Buffer, c *cobra.Command) {
	fmt.Fprintf(buf, "## %s\n\n", c.CommandPath())
	if c.Short != "" {
		fmt.Fprintf(buf, "%s.\n\n", trimSentence(c.Short))
	}
	if long := strings.TrimSpace(c.Long); long != "" && long != strings.TrimSpace(c.Short) {
		fmt.Fprintf(buf, "%s\n\n", long)
	}
	if c.Runnable() {
		fmt.Fprintf(buf, "```\n%s\n```\n\n", c.UseLine())
	}
	if usage := c.LocalFlags().FlagUsages(); strings.TrimSpace(usage) != "" {
		fmt.Fprintf(buf, "Flags:\n\n```\n%s```\n\n", usage)
	}
}

// trimSentence drops a trailing period so we can append our own and
// avoid the doubled "Run a projection locally.." that cobra's Short
// fields produce when a maintainer remembered the period.
func trimSentence(s string) string {
	return strings.TrimRight(strings.TrimSpace(s), ".")
}
