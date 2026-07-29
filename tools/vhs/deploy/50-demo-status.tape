# The status table after a deploy: runtime state, drift verdicts, and
# deploy attribution. Emits a still.
#
# Runs from the seeded tape workspace; the number prefix orders the tapes,
# because recordings mutate server state as they go. Regenerate via the
# "Regenerate demo recordings" workflow, or locally:
#   just screenshots db-up && just screenshots prepare-tapes
#   cd tools/screenshots/.workspace && VHS_NO_SANDBOX=1 vhs ../../vhs/deploy/50-demo-status.tape

# The still is the committed artifact; the gif is a scratch requirement.
Output ../out/vhs-demo-status-scratch.gif

Set FontSize 18
Set Width 1600
Set Height 360
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

Type "gaffer status"
Enter
Sleep 3500ms
Screenshot ../../../docs/public/demo-status.png
Sleep 500ms
