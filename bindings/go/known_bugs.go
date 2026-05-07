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

// KnownBug describes a KurrentDB upstream bug that gaffer reproduces. The
// runtime is the source of truth; consumers (CLI, MCP) call KnownBugs() to
// fetch the current registry.
//
// FixedIn is nil when no upstream fix has shipped (the bug fires in every
// configuration). When set, it's a MAJOR.MINOR.PATCH string; setting
// dbVersion >= FixedIn disables the bug.
type KnownBug struct {
	Code        string  `json:"code"`
	Description string  `json:"description"`
	FixedIn     *string `json:"fixedIn,omitempty"`
}

// KnownBugs returns the runtime's registry of compat-tracked bugs as a slice.
// Loaded each call from the C runtime; cache at the call site if used hot.
func KnownBugs() ([]KnownBug, error) {
	cStr := C.gaffer_known_bugs()
	if cStr == nil {
		return nil, fmt.Errorf("gaffer_known_bugs returned null")
	}
	defer C.gaffer_free(unsafe.Pointer(cStr))

	var bugs []KnownBug
	if err := json.Unmarshal([]byte(C.GoString(cStr)), &bugs); err != nil {
		return nil, fmt.Errorf("decoding known bugs: %w", err)
	}
	return bugs, nil
}
