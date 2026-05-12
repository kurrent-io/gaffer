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
	// Shape is populated only when the FFI caller set
	// `includeShape: true` on the session options. Nil otherwise.
	Shape *ProjectionShape `json:"shape,omitempty"`
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

// ProjectionShape is the structural snapshot the runtime walker
// produces when the FFI caller sets `includeShape: true` in the
// session options. Mirrors the wire shape from
// Gaffer.Sdk.ProjectionShape. Raw counts here; the gaffer CLI's
// telemetry helper buckets these at telemetry-emit time.
//
// Note: this is the FFI binding layer, distinct from the gaffer
// CLI's telemetry-wire ProjectionShape (which uses bucketed
// RawCount fields). The CLI translates between the two.
type ProjectionShape struct {
	Parsable      bool                         `json:"parsable"`
	FileSize      int                          `json:"fileSize"`
	Handlers      ProjectionShapeHandlers      `json:"handlers"`
	BuiltinCounts ProjectionShapeBuiltinCounts `json:"builtinCounts"`
}

// ProjectionShapeHandlers carries which handler kinds the
// projection registers, plus the raw count of distinct event-name
// handlers (the names themselves never cross the FFI).
type ProjectionShapeHandlers struct {
	Any                bool `json:"any"`
	Init               bool `json:"init"`
	Deleted            bool `json:"deleted"`
	DistinctEventNames int  `json:"distinctEventNames"`
}

// ProjectionShapeBuiltinCounts holds raw call counts per allowlisted
// projection builtin. Sparse: a builtin not called is absent in the
// JSON (the .NET serializer omits null fields), which decodes here
// as a nil pointer.
type ProjectionShapeBuiltinCounts struct {
	FromAll        *int `json:"fromAll,omitempty"`
	FromStream     *int `json:"fromStream,omitempty"`
	FromStreams    *int `json:"fromStreams,omitempty"`
	FromCategory   *int `json:"fromCategory,omitempty"`
	FromCategories *int `json:"fromCategories,omitempty"`
	When           *int `json:"when,omitempty"`
	ForeachStream  *int `json:"foreachStream,omitempty"`
	OutputState    *int `json:"outputState,omitempty"`
	TransformBy    *int `json:"transformBy,omitempty"`
	PartitionBy    *int `json:"partitionBy,omitempty"`
	Emit           *int `json:"emit,omitempty"`
	LinkTo         *int `json:"linkTo,omitempty"`
	CopyTo         *int `json:"copyTo,omitempty"`
	// deprecated.
	LinkStreamTo  *int `json:"linkStreamTo,omitempty"`
	ChainHandlers *int `json:"chainHandlers,omitempty"`
	UpdateOf      *int `json:"updateOf,omitempty"`
}
