// Projection that demonstrates how `gaffer dev` surfaces quirks - both at
// compile time (a heads-up in the info header) and at runtime (inline, at the
// point each one fires while processing an event).
//
// Two quirks show up:
//
// - quirk.log.multiParam: calling log() with multiple arguments trips an
//   upstream quirk in how the args are rendered. gaffer detects it statically
//   (a [warning] in the header, with a source location) AND fires it again at
//   runtime on every Tick, inline at the log() call - the heads-up says "you
//   have this", the inline warning says "you just hit it".
//
// - quirk.serialize.rawString: returning a bare string as state persists it
//   un-encoded (e.g. `milestone reached`, not `"milestone reached"`), so the
//   projection would fault on reload when JSON.parse runs on the stored value.
//   This is value-dependent, so it can only be caught at runtime, with no
//   source location - here it fires on the third event, where the handler
//   returns a bare string instead of an object.
fromAll().when({
  $init() {
    return { seen: 0 };
  },
  Tick(state) {
    const seen = (state.seen || 0) + 1;
    log("tick", seen);
    if (seen === 3) {
      return "milestone reached";
    }
    return { seen };
  },
});
