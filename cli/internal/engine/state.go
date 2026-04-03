package engine

import (
	"encoding/json"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
)

type StateSummary struct {
	Partitioned   bool
	Partitions    map[string]PartitionState
	State         json.RawMessage
	Result        json.RawMessage
	SharedState   json.RawMessage
	HasTransforms bool
	HasBiState    bool
}

type PartitionState struct {
	State  json.RawMessage
	Result json.RawMessage
}

func CollectState(session *gafferruntime.Session, info gafferruntime.QuerySources, partitions map[string]bool) StateSummary {
	isPartitioned := info.ByStreams || info.ByCustomPartitions

	summary := StateSummary{
		Partitioned:   isPartitioned,
		HasTransforms: info.DefinesStateTransform,
		HasBiState:    info.IsBiState,
	}

	if isPartitioned {
		summary.Partitions = make(map[string]PartitionState)
		for partition := range partitions {
			ps := PartitionState{}
			if state := session.GetState(&partition); state != nil {
				ps.State = json.RawMessage(*state)
			}
			if info.DefinesStateTransform {
				if result, err := session.GetResult(&partition); err == nil && result != nil {
					ps.Result = json.RawMessage(*result)
				}
			}
			summary.Partitions[partition] = ps
		}
	} else {
		if state := session.GetState(nil); state != nil {
			summary.State = json.RawMessage(*state)
		}
		if info.DefinesStateTransform {
			if result, err := session.GetResult(nil); err == nil && result != nil {
				summary.Result = json.RawMessage(*result)
			}
		}
	}

	if info.IsBiState {
		if shared := session.GetSharedState(); shared != nil {
			summary.SharedState = json.RawMessage(*shared)
		}
	}

	return summary
}
