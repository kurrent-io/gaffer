package cmd

import (
	"sync"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
)

// Compat-bug registry, loaded once from the runtime via KnownBugs() and
// cached for the lifetime of the CLI process. Used by fatal-error rendering
// to enrich a CompatCode with description + fixedIn.
//
// Loading is best-effort: if the runtime call fails we fall back to an
// empty cache, which means the CLI surfaces the bare CompatCode without
// the registry-driven hint.
var (
	knownBugsOnce   sync.Once
	knownBugsByCode map[string]gafferruntime.KnownBug
)

func compatBugLookup(code string) (gafferruntime.KnownBug, bool) {
	knownBugsOnce.Do(func() {
		bugs, err := gafferruntime.KnownBugs()
		if err != nil {
			return
		}
		knownBugsByCode = make(map[string]gafferruntime.KnownBug, len(bugs))
		for _, b := range bugs {
			knownBugsByCode[b.Code] = b
		}
	})
	b, ok := knownBugsByCode[code]
	return b, ok
}
