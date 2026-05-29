// Projection that demonstrates how `gaffer dev` surfaces quirks - both at
// compile time (a heads-up in the info header) and at runtime (inline, at the
// point each one fires while processing an event).
//
// Two quirks show up:
//
// - compat.log.multiParam: calling log() with multiple arguments trips an
//   upstream quirk in how the args are rendered. gaffer detects it statically
//   (a [warning] in the header, with a source location) AND fires it again at
//   runtime on every Tick, inline at the log() call - the heads-up says "you
//   have this", the inline warning says "you just hit it".
//
// - compat.biState.stringSlot: KurrentDB JSON-quotes a raw string written to a
//   biState slot on persistence, storing "milestone reached" as
//   `"\"milestone reached\""` rather than passing it through. This is
//   value-dependent, so it can only be caught at runtime, with no source
//   location. The event counter lives in the SHARED slot (always an object),
//   leaving per-partition slot 0 free to hold a raw string on the third event.
fromAll().when({
  $init() {
    return { seen: 0 };
  },
  $initShared() {
    return { count: 0 };
  },
  Tick([state, shared], event) {
    shared.count += 1;
    log("tick", shared.count);
    if (shared.count === 3) {
      return ["milestone reached", shared];
    }
    return [{ seen: shared.count }, shared];
  },
});
