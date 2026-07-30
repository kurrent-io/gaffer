# The interactive history timeline: walk the entries, open an entry's
# diff with d, and reach the rollback confirm with r (cancelled).
#
# Runs from the seeded tape workspace; the number prefix orders the tapes,
# because recordings mutate server state as they go. Regenerate via the
# "Regenerate demo recordings" workflow, or locally:
#   just screenshots db-up && just screenshots prepare-tapes
#   cd tools/screenshots/.workspace && VHS_NO_SANDBOX=1 vhs ../../vhs/deploy/30-demo-history.tape

Output ../../../docs/public/demo-history.gif

Set FontSize 14
Set Width 1200
Set Height 700
Set Padding 20
Set Framerate 24
Set TypingSpeed 50ms
Set Theme "Catppuccin Mocha"

# Suppress the first-run telemetry disclosure banner and the update notice,
# and force colour under ttyd (see demo-dev.tape for the why of each).
Env GAFFER_TELEMETRY_OPTOUT "1"
Env GAFFER_NO_UPDATE_CHECK "1"
Env CLICOLOR_FORCE "1"
Env COLORTERM "truecolor"

# Clean, consistent prompt across all demo recordings.
Hide
Type "export PS1='$ '"
Enter
Ctrl+L
Show

Type "gaffer history order-count"
Enter
Sleep 3s
Down
Sleep 900ms
Down
Sleep 900ms
Type "d"
Sleep 2500ms
Down
Sleep 1500ms
Escape
Sleep 900ms
Up
Sleep 900ms
Type "r"
Sleep 2500ms
Escape
Sleep 900ms
Type "q"
Sleep 1500ms
