package gafferruntime

/*
#include "gaffer.h"
*/
import "C"

import (
	"encoding/json"
	"fmt"
	"unsafe"
)

// KnownQuirk describes a KurrentDB upstream quirk that gaffer reproduces. The
// runtime is the source of truth; consumers (CLI, MCP) call KnownQuirks() to
// fetch the current registry.
//
// FixedIn is nil when no upstream fix has shipped (the quirk fires in every
// configuration). When set, it's a MAJOR.MINOR.PATCH string; setting
// quirksVersion >= FixedIn disables the quirk.
type KnownQuirk struct {
	Code        string  `json:"code"`
	Description string  `json:"description"`
	FixedIn     *string `json:"fixedIn,omitempty"`
}

// KnownQuirks returns the runtime's registry of compat-tracked quirks as a slice.
// Loaded each call from the C runtime; cache at the call site if used hot.
func KnownQuirks() ([]KnownQuirk, error) {
	cStr := C.gaffer_known_quirks()
	if cStr == nil {
		return nil, fmt.Errorf("gaffer_known_quirks returned null")
	}
	defer C.gaffer_free(unsafe.Pointer(cStr))

	var quirks []KnownQuirk
	if err := json.Unmarshal([]byte(C.GoString(cStr)), &quirks); err != nil {
		return nil, fmt.Errorf("decoding known quirks: %w", err)
	}
	return quirks, nil
}
