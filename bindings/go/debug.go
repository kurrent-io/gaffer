package gafferruntime

// BreakInfo describes a debug pause.
type BreakInfo struct {
	Reason string
	Source string
	Line   int
	Column int
}

// DebugCallFrame is a single frame in the call stack.
type DebugCallFrame struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

// DebugScopeInfo describes a scope in a call frame.
type DebugScopeInfo struct {
	Name               string `json:"name"`
	VariablesReference int    `json:"variablesReference"`
	Expensive          bool   `json:"expensive"`
}

// DebugVariable is a single variable in a scope.
type DebugVariable struct {
	Name               string `json:"name"`
	Value              string `json:"value"`
	Type               string `json:"type"`
	VariablesReference int    `json:"variablesReference"`
}
