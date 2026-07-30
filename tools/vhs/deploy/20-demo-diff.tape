# A drifted projection's deployed-vs-local diff: the verdict and the
# inline source diff with the changed span highlighted. Emits a still.
#
# Runs from the seeded tape workspace; the number prefix orders the tapes,
# because recordings mutate server state as they go. Regenerate via the
# "Regenerate demo recordings" workflow, or locally:
#   just screenshots db-up && just screenshots prepare-tapes
#   cd tools/screenshots/.workspace && VHS_NO_SANDBOX=1 vhs ../../vhs/deploy/20-demo-diff.tape

# The still is the committed artifact; the gif is a scratch requirement.
Output ../out/vhs-demo-diff-scratch.gif

Set FontSize 16
Set Width 1400
Set Height 640
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

Type "gaffer diff order-count"
Enter
Sleep 3500ms
Screenshot ../../../docs/public/demo-diff.png
Sleep 500ms
