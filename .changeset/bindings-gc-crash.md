---
"@kurrent/gaffer": patch
---

The Go runtime bindings no longer risk a rare fatal crash (`invalid pointer found on stack`) under GC pressure. The runtime's integer session handles could appear in pointer-typed stack slots during FFI calls; if the GC moved the stack while a callback was running mid-call, the process aborted. Handles now stay integer-typed end-to-end on the Go side, with all casting done in C shims. The crash could hit anything embedding the bindings, including the CLI.
