package gafferruntime

// ProjectionInfo describes a projection's source configuration and features.
// Returned by the runtime via the SDK boundary.
type ProjectionInfo struct {
	AllStreams                       bool     `json:"allStreams"`
	AllEvents                        bool     `json:"allEvents"`
	Categories                       []string `json:"categories"`
	Streams                          []string `json:"streams"`
	Events                           []string `json:"events"`
	ByStreams                        bool     `json:"byStreams"`
	ByCustomPartitions               bool     `json:"byCustomPartitions"`
	BiState                          bool     `json:"biState"`
	DefinesHandlers                  bool     `json:"definesHandlers"`
	DefinesStateTransform            bool     `json:"definesStateTransform"`
	ProducesResults                  bool     `json:"producesResults"`
	HandlesDeletedNotifications      bool     `json:"handlesDeletedNotifications"`
	IncludeLinks                     bool     `json:"includeLinks"`
	ResultStreamName                 *string  `json:"resultStreamName"`
	PartitionResultStreamNamePattern *string  `json:"partitionResultStreamNamePattern"`
	ReorderEvents                    bool     `json:"reorderEvents"`
	ProcessingLag                    *int     `json:"processingLag"`
}
