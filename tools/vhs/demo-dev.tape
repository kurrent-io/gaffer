# Demo recording for cli/README.md.
# Regenerate from the demo/ directory with
#   vhs ../tools/vhs/demo-dev.tape
# or via the "Regenerate demo recordings" workflow on GitHub.

Output ../assets/demo-dev.gif

# Suppress the first-run telemetry disclosure banner so it doesn't
# appear in the recording. CI runs in a fresh environment each time,
# so without this the banner shows on every regen.
Env GAFFER_TELEMETRY_OPTOUT "1"

Set FontSize 14
Set Width 1200
Set Height 700
Set Padding 20
Set TypingSpeed 50ms
Set Theme "Catppuccin Mocha"

Type "gaffer info order-count"
Enter
Sleep 2500ms

Type "gaffer dev order-count --fixture happy"
Enter
Sleep 4s
