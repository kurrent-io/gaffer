# Demo recording for the docs (getting started / interactive mode).
# Regenerate from the demo/ directory with
#   vhs ../tools/vhs/demo-init.tape
# or via the "Regenerate demo recordings" workflow on GitHub.

Output ../assets/demo-init.gif

# Suppress the first-run telemetry disclosure banner so it doesn't
# appear in the recording.
Env GAFFER_TELEMETRY_OPTOUT "1"
# Don't let the once-a-day "update available" notice appear mid-recording.
Env GAFFER_NO_UPDATE_CHECK "1"
# Force colour: under ttyd in CI the charm libs detect no colour support
# and render the prompts unstyled, so force a truecolor profile.
Env CLICOLOR_FORCE "1"
Env COLORTERM "truecolor"

Set FontSize 14
Set Width 1200
Set Height 600
Set Padding 20
Set TypingSpeed 50ms
Set Theme "Catppuccin Mocha"

# `gaffer init` refuses to run where a gaffer.toml already exists (the
# demo/ dir has one), so record in a fresh temp directory with a clean
# prompt. Hidden so only the gaffer interaction shows.
Hide
Type "export PS1='$ ' && cd $(mktemp -d)"
Enter
Ctrl+L
Show

Type "gaffer init"
Enter
Sleep 1.5s
Down
Sleep 800ms
Up
Sleep 1s
Enter
Sleep 2.5s
