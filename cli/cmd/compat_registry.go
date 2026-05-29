package cmd

import (
	"sync"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
)

// Compat-quirk registry, loaded once from the runtime via KnownQuirks() and
// cached for the lifetime of the CLI process. Used by fatal-error rendering
// to enrich a CompatCode with description + fixedIn.
//
// Loading is best-effort: if the runtime call fails we fall back to an
// empty cache, which means the CLI surfaces the bare CompatCode without
// the registry-driven hint.
var (
	knownQuirksOnce   sync.Once
	knownQuirksByCode map[string]gafferruntime.KnownQuirk
)

func compatQuirkLookup(code string) (gafferruntime.KnownQuirk, bool) {
	knownQuirksOnce.Do(func() {
		quirks, err := gafferruntime.KnownQuirks()
		if err != nil {
			return
		}
		knownQuirksByCode = make(map[string]gafferruntime.KnownQuirk, len(quirks))
		for _, b := range quirks {
			knownQuirksByCode[b.Code] = b
		}
	})
	b, ok := knownQuirksByCode[code]
	return b, ok
}
