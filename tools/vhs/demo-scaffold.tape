# Demo recording for the docs (getting started / interactive mode).
# Regenerate from the demo/ directory with
#   VHS_NO_SANDBOX=1 vhs ../tools/vhs/demo-scaffold.tape  # NO_SANDBOX needed in a devcontainer only
# or via the "Regenerate demo recordings" workflow on GitHub.

Output ../docs/public/demo-scaffold.gif

# Set directives must precede Env or vhs silently ignores them (the theme
# and size below only started applying once this order was fixed).
Set FontSize 14
Set Width 1200
Set Height 700
Set Padding 20
Set TypingSpeed 50ms
Set Theme "Catppuccin Mocha"

Env GAFFER_TELEMETRY_OPTOUT "1"
# Don't let the once-a-day "update available" notice appear mid-recording.
Env GAFFER_NO_UPDATE_CHECK "1"
# Force colour: under ttyd in CI the charm libs detect no colour support
# and render the prompts unstyled, so force a truecolor profile.
Env CLICOLOR_FORCE "1"
Env COLORTERM "truecolor"

# Record in a fresh project so the run is repeatable and doesn't mutate
# demo/. The init is hidden setup; only the scaffold interaction shows.
# Silence init and let it finish before Show so its "Initialized..."
# line can't leak into the recording.
Hide
Type "export PS1='$ ' && cd $(mktemp -d) && gaffer init >/dev/null 2>&1"
Enter
Sleep 1s
Ctrl+L
Show

Type "gaffer scaffold"
Enter
Sleep 1.5s
Type "projections/order-totals.js"
Sleep 600ms
Enter
Sleep 1s
# Source: move to "A single stream"
Down
Sleep 800ms
Enter
Sleep 1s
Type "orders"
Sleep 600ms
Enter
Sleep 1s
# Partitioning is skipped for a single stream (per-stream is invalid
# there), so the next prompt is the emit toggle. Accept the default (No).
Enter
Sleep 1s
# Engine version: accept the default (2, recommended).
Enter
Sleep 2.5s
