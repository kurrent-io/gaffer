---
"gaffer-vscode": patch
---

The deploy-history viewer now uses a two-pane layout. The timeline stays on the left; selecting a version opens a detail panel on the right showing its metadata (when, run state, content hash, actor, tool, operation, and source) and its actions. The diff-previous, diff-local, and roll-back actions moved off each row into that panel. Timeline rows are keyboard-navigable, and the panel drops below the timeline on a narrow editor.
