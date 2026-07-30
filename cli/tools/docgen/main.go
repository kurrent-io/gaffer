// docgen generates a single Markdown reference page covering every
// visible gaffer subcommand, for inclusion in the user-facing docs.
//
//	go run ./tools/docgen <output-file>
//
// The page mirrors `gaffer --help`: commands appear under the same
// group headings, in the same order, both read from the cobra command
// tree rather than a hard-coded list. Hidden commands and flags are
// skipped. The root `gaffer` command is not emitted; its short
// description is assumed to live in the page's frontmatter or intro.
package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kurrent-io/gaffer/cli/cmd"
)

const frontmatter = `---
title: Commands
description: Full reference for every gaffer subcommand and its flags.
---

Full reference for every gaffer subcommand, grouped the way ` + "`gaffer --help`" + ` presents them. Generated from the CLI source; run ` + "`just gen-docs`" + ` to refresh after touching a command.

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

	// Root's persistent flags apply to every command but appear under
	// none of them (writeCommand emits local flags only), so surface
	// them once up front. FlagUsages skips hidden flags.
	if usage := root.PersistentFlags().FlagUsages(); strings.TrimSpace(usage) != "" {
		fmt.Fprintf(&buf, "Global flags, accepted by every command:\n\n```\n%s```\n\n", usage)
	}

	groups := make(map[string]bool)
	for _, group := range root.Groups() {
		groups[group.ID] = true
		fmt.Fprintf(&buf, "## %s\n\n", group.Title)
		for _, sub := range root.Commands() {
			if sub.GroupID != group.ID || sub.Hidden || !sub.IsAvailableCommand() {
				continue
			}
			writeCommand(&buf, sub)
		}
	}
	// A visible command outside every registered group would be silently
	// dropped by the loop above (cobra only validates GroupID on Execute,
	// which docgen never calls) - fail generation instead.
	for _, sub := range root.Commands() {
		if !groups[sub.GroupID] && !sub.Hidden && sub.IsAvailableCommand() {
			fmt.Fprintf(os.Stderr, "command not in a registered group: %s\n", sub.Name())
			os.Exit(1)
		}
	}

	if err := os.WriteFile(out, buf.Bytes(), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// writeCommand emits c and its visible descendants, pre-order, each as
// an H3 so every command sits directly under its group heading and
// keeps a `#gaffer-<cmd>` anchor in the page TOC. Nested subcommands
// keep cobra's order, which is registration order (root.go turns
// command sorting off).
func writeCommand(buf *bytes.Buffer, c *cobra.Command) {
	fmt.Fprintf(buf, "### %s\n\n", c.CommandPath())
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
	for _, sub := range c.Commands() {
		if sub.Hidden || !sub.IsAvailableCommand() {
			continue
		}
		writeCommand(buf, sub)
	}
}

// trimSentence drops a trailing period so we can append our own and
// avoid the doubled "Run a projection locally.." that cobra's Short
// fields produce when a maintainer remembered the period.
func trimSentence(s string) string {
	return strings.TrimRight(strings.TrimSpace(s), ".")
}
