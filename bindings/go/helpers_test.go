package gafferruntime

// Default engine version options used in tests. All non-fixture tests
// must specify engineVersion explicitly via the runtime FFI.
var (
	v2Opts = `{"engineVersion":2}`
	v1Opts = `{"engineVersion":1}`
)
