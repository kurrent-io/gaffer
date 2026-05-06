package gafferruntime

// ProjectionInfo describes a projection's source configuration and features.
// Returned by the runtime via the SDK boundary.
type ProjectionInfo struct {
	AllStreams                       bool         `json:"allStreams"`
	AllEvents                        bool         `json:"allEvents"`
	Categories                       []string     `json:"categories"`
	Streams                          []string     `json:"streams"`
	Events                           []string     `json:"events"`
	ByStreams                        bool         `json:"byStreams"`
	ByCustomPartitions               bool         `json:"byCustomPartitions"`
	BiState                          bool         `json:"biState"`
	DefinesHandlers                  bool         `json:"definesHandlers"`
	DefinesStateTransform            bool         `json:"definesStateTransform"`
	ProducesResults                  bool         `json:"producesResults"`
	HandlesDeletedNotifications      bool         `json:"handlesDeletedNotifications"`
	IncludeLinks                     bool         `json:"includeLinks"`
	ResultStreamName                 *string      `json:"resultStreamName"`
	PartitionResultStreamNamePattern *string      `json:"partitionResultStreamNamePattern"`
	ReorderEvents                    bool         `json:"reorderEvents"`
	ProcessingLag                    *int         `json:"processingLag"`
	Diagnostics                      []Diagnostic `json:"diagnostics"`
}

// Diagnostic is a compile-time diagnostic emitted by the runtime.
//
// Code is namespaced as "<category>.<name>" (e.g. "deprecated.linkStreamTo").
// Range is nil if the diagnostic has no associated source location.
type Diagnostic struct {
	Code     string             `json:"code"`
	Message  string             `json:"message"`
	Severity DiagnosticSeverity `json:"severity"`
	Range    *SourceRange       `json:"range"`
}

// DiagnosticSeverity matches the LSP DiagnosticSeverity enum so editor
// adapters can pass values through unchanged.
type DiagnosticSeverity int

const (
	DiagnosticSeverityError       DiagnosticSeverity = 1
	DiagnosticSeverityWarning     DiagnosticSeverity = 2
	DiagnosticSeverityInformation DiagnosticSeverity = 3
	DiagnosticSeverityHint        DiagnosticSeverity = 4
)

// SourceRange covers a span in projection source. Inclusive start, exclusive
// end - matches LSP and most editor APIs.
type SourceRange struct {
	Start SourcePosition `json:"start"`
	End   SourcePosition `json:"end"`
}

// SourcePosition is a 1-based line and column in projection source. LSP
// clients subtract 1 from each at the boundary.
type SourcePosition struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}
