package gafferruntime

// QuerySources describes a projection's source configuration and features.
// Parsed from the runtime's internal source definition.
type QuerySources struct {
	AllStreams                       bool     `json:"AllStreams"`
	AllEvents                        bool     `json:"AllEvents"`
	Categories                       []string `json:"Categories"`
	Streams                          []string `json:"Streams"`
	Events                           []string `json:"Events"`
	ByStreams                        bool     `json:"ByStreams"`
	ByCustomPartitions               bool     `json:"ByCustomPartitions"`
	IsBiState                        bool     `json:"IsBiState"`
	DefinesFold                      bool     `json:"DefinesFold"`
	DefinesStateTransform            bool     `json:"DefinesStateTransform"`
	ProducesResults                  bool     `json:"ProducesResults"`
	HandlesDeletedNotifications      bool     `json:"HandlesDeletedNotifications"`
	IncludeLinks                     bool     `json:"IncludeLinks"`
	ResultStreamName                 string   `json:"ResultStreamName"`
	PartitionResultStreamNamePattern string   `json:"PartitionResultStreamNamePattern"`
	ReorderEvents                    bool     `json:"ReorderEvents"`
	ProcessingLag                    *int     `json:"ProcessingLag"`
}
