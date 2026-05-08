# Gaffer telemetry

`gaffer` (the projection toolkit for KurrentDB) sends pseudonymous usage data and crash reports to help us improve the tool. This page describes exactly what we collect, how it travels, and how to turn it off.

This document changes when behaviour changes; updates are public commits to the [gaffer repository](https://github.com/kurrent-io/gaffer).

## TL;DR

- A per-install random identifier (no account, no email, no PII).
- Counts of work done (bucketed: "100-999 events processed", not "847 events processed").
- The shape of your projections (which builtins called, file size tier). Never names, paths, or contents.
- Crash reports from gaffer's own code, with file paths and any user-JS frames removed by construction.
- Stored in PostHog's EU data centre.
- One line in `gaffer.toml`, one config command, or any of three environment variables silences it.

## How to disable

Any one of these silences telemetry. They're checked in this order; the first hit wins.

1. **Project-level**: add `telemetry = false` to the top of `gaffer.toml`. Covers every gaffer invocation in that repo, including CI.
2. **User-level**: run `gaffer config telemetry off`. Persists across projects on this machine.
3. **Environment variable**: set any of the following to a truthy value (`1`, `true`, `yes`, `on`):
   - `GAFFER_TELEMETRY_OPTOUT`
   - `KURRENTDB_TELEMETRY_OPTOUT` (already set by users who opted out of KurrentDB telemetry; covers gaffer for free)
   - `DO_NOT_TRACK` (the open-source convention; see [consoledonottrack.com](https://consoledonottrack.com))
4. **VS Code's `telemetry.telemetryLevel`**: the gaffer extension respects this VS Code setting in addition to the above.

## What we collect

Four event types, all carrying only the fields below. The full machine-readable schema lives in the [gaffer repository](https://github.com/kurrent-io/gaffer/tree/main/telemetry/schemas).

### `command_invoked`

Fires once at the end of every `gaffer` CLI invocation. Records:

- **Which command ran** (`version`, `init`, `dev`, `mcp`, etc.).
- **Wall-clock duration** of the invocation, bucketed (none / 1 / 2-9 / 10-99 / 100-999 / 1000+ ms).
- **Outcome**: `success`, `user_interrupt`, `internal_error`, etc.
- **Bucketed counts of work done** that depend on the command (number of projections in the manifest, tool calls during an MCP session, breakpoints set during a debug session, etc.). Always bucketed, never exact.
- **How the run was triggered**: directly from a terminal, from VS Code, from an MCP client.

### `projection_shape`

A snapshot of what one of your projection files looks like, structurally. Emitted when gaffer first encounters a projection and again only if its bucketed shape changes. Records:

- **An opaque hashed identifier** (per-install, salted, non-reversible) for the projection file.
- **Bucketed file size tier** (under 1KB, 1KB-5KB, etc.).
- **Which gaffer builtins it calls** (`fromAll`, `when`, `partitionBy`, etc.) with bucketed call counts.
- **Whether it registers `$any` / `$init` / `$deleted` handlers** and a bucketed count of how many distinct event-name handlers it has.

The names of events it handles, the names of streams it reads or writes, the function names you've defined, the contents of any string, and the file path itself are all out of scope. The walker only sees gaffer-builtin call sites; everything else is invisible.

### `extension_activated`

Fires once when the VS Code extension activates. Records:

- **Whether the gaffer CLI binary is reachable** (PATH-resolvable, spawnable, responds to `gaffer version` within a timeout). The "broken install" diagnostic.
- **Editor and gaffer versions**.
- **Bucketed activation duration**.

### `exception`

A crash in gaffer's own code (a Go panic in the CLI, an unhandled JS error in the extension host, a runtime exception in the projection engine). Records:

- **Exception type and message** - always a message gaffer wrote, never a propagated message from your projection code.
- **Stack frames** - file basenames only (never full paths), function names from gaffer's code only (user-JS frames are dropped entirely).
- **Coarse lifecycle phase** the crash happened in.

Errors thrown by your projection code (a `TypeError`, a `ReferenceError`, etc.) are not in scope here - they surface as a structural outcome on the relevant `command_invoked` event, with no message body or stack.

## What we never collect

- Your projection source, in any form (contents, paths, hashes of contents).
- Stream names, event names, category names, result-stream patterns.
- Function names, variable names, or any identifier from your code.
- File paths beyond the basename inside scrubbed stack frames.
- KurrentDB connection strings, hostnames, or credentials.
- Environment variable names or values (we read opt-out vars to drive behaviour, but they don't become telemetry).
- User account or OS user information.
- Hardware identifiers (MAC addresses, machine IDs, etc.).
- IP addresses (Cloudflare sees them transiently; the worker doesn't forward them).

## How it travels

1. The CLI / extension constructs a JSON envelope, validates the shape locally, and POSTs it to `https://telemetry.gaffer.kurrent.io/v1/ingest`.
2. A Cloudflare Worker (open source, in this repo) validates the envelope against the schema and translates it to the format PostHog expects.
3. The worker forwards the translated batch to PostHog's EU data centre.

The worker has no persistent storage on the request path beyond Cloudflare's standard transit logs (~30 days, includes IPs). Events are stored in PostHog EU.

## Identifiers we use

- `telemetry_id`: a UUID generated when you install gaffer. Per-install, persists across invocations until you opt out (which clears it). No relation to your account, email, or any other identifier.
- `salt`: a per-install secret used to hash projection paths into opaque identifiers. Stays local; never sent on the wire.
- `run_id`: a per-process UUID. Correlates events from a single CLI invocation or extension activation. Discarded when the process exits.
- `session_id`: stamped by the worker (not the client). Groups events from your activity into "sessions" using a 30-minute inactivity window.

## Right to be forgotten

- **Email `privacy@kurrent.io`** with your `telemetry_id`. We'll delete all events associated with it from PostHog within 30 days (GDPR's standard timeframe).
- **Capturing your id**: `gaffer config telemetry status` prints it while you're opted in. `gaffer config telemetry off` prints it one last time before clearing local state.

Deletion scope:

- **PostHog rows**: deleted via `$delete_person` on receipt.
- **D1 session-stitching tables**: roll off naturally on a 30-minute / 25-hour TTL; nothing to delete.
- **Cloudflare access logs**: roll off CF's standard ~30-day retention.

## Why opt-out, not opt-in

We're aware this is a defensible default rather than an obvious one. The data we collect is bucketed, allowlisted, and doesn't identify you or your projects - the safety isn't a scrubbing pass we hope catches everything, it's a property of the schema. Multiple opt-out channels (including ones you've already configured for other tools) silence it. The schema and worker are public; the disclosure (this page) is too. Legal frame is GDPR Article 6(1)(f) "legitimate interest" - the shape of the data is what makes that frame fit, not the other way around.

## Questions, concerns, complaints

Open an issue at [github.com/kurrent-io/gaffer](https://github.com/kurrent-io/gaffer/issues) or email `privacy@kurrent.io`.
