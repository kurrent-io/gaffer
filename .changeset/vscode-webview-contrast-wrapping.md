---
"gaffer-vscode": patch
---

The deploy plan and history webviews render correctly across editor themes:

- The "logic change" tag uses the theme's paired warning foreground, so it stays legible on themes that tint the warning background.
- Disabled buttons keep their label readable on light themes: an untinted pill with the theme's disabled foreground, instead of half-opacity over a tinted background.
- Long diagnostics in the deploy plan wrap in place instead of scrolling sideways.
- The history detail panel takes the width its content needs from the timeline (up to a cap), keeping its action buttons on one line where there's room.
