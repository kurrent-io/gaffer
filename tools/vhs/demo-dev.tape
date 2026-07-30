# Demo recording for cli/README.md.
# Regenerate from the demo/ directory with
#   VHS_NO_SANDBOX=1 vhs ../tools/vhs/demo-dev.tape  # NO_SANDBOX needed in a devcontainer only
# or via the "Regenerate demo recordings" workflow on GitHub.

Output ../assets/demo-dev.gif

Set FontSize 14
Set Width 1200
Set Height 700
Set Padding 20
Set Framerate 24
Set TypingSpeed 50ms
Set Theme "Catppuccin Mocha"

# Suppress the first-run telemetry disclosure banner so it doesn't
# appear in the recording. CI runs in a fresh environment each time,
# so without this the banner shows on every regen.
Env GAFFER_TELEMETRY_OPTOUT "1"
# Don't let the once-a-day "update available" notice appear mid-recording.
Env GAFFER_NO_UPDATE_CHECK "1"
# Colour under ttyd in CI: the runner exports CI=true, and termenv treats
# any CI environment as a non-terminal - profile falls to Ascii, and
# CLICOLOR_FORCE alone only rescues 16-colour ANSI, which collapses the
# CLI's truecolor diff washes onto the theme palette. Clear CI so the pty
# is trusted, then COLORTERM selects the truecolor profile.
Env CI ""
Env CLICOLOR_FORCE "1"
Env COLORTERM "truecolor"

# Clean, consistent prompt across all demo recordings.
Hide
Type "export PS1='$ '"
Enter
Ctrl+L
Show

Type "gaffer info order-count"
Enter
Sleep 2500ms

Type "gaffer dev order-count --fixture happy"
Enter
Sleep 4s
