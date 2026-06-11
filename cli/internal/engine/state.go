package engine

import (
	"encoding/json"
	"fmt"

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

// CollectState reads the session's current state, result, and shared state
// into a StateSummary. It fills best-effort: every field that reads cleanly is
// populated, and the first getter error encountered is returned alongside the
// (partial) summary so callers can choose to surface it or display what was
// collected. The only getter that can error in practice is GetResult, which
// runs the V1 transformBy/filterBy JS; GetState/GetSharedState are cache reads.
func CollectState(session *gafferruntime.Session, info gafferruntime.ProjectionInfo, partitions map[string]bool) (StateSummary, error) {
	isPartitioned := info.ByStreams || info.ByCustomPartitions

	summary := StateSummary{
		Partitioned:   isPartitioned,
		HasTransforms: info.DefinesStateTransform,
		HasBiState:    info.BiState,
	}

	var firstErr error
	keep := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	if isPartitioned {
		summary.Partitions = make(map[string]PartitionState)
		for partition := range partitions {
			ps := PartitionState{}
			state, err := session.GetState(&partition)
			keep(wrapStateErr(err, "state", partition))
			if err == nil && state != nil {
				ps.State = json.RawMessage(*state)
			}
			if info.DefinesStateTransform {
				result, err := session.GetResult(&partition)
				keep(wrapStateErr(err, "result", partition))
				if err == nil && result != nil {
					ps.Result = json.RawMessage(*result)
				}
			}
			summary.Partitions[partition] = ps
		}
	} else {
		state, err := session.GetState(nil)
		keep(wrapStateErr(err, "state", ""))
		if err == nil && state != nil {
			summary.State = json.RawMessage(*state)
		}
		if info.DefinesStateTransform {
			result, err := session.GetResult(nil)
			keep(wrapStateErr(err, "result", ""))
			if err == nil && result != nil {
				summary.Result = json.RawMessage(*result)
			}
		}
	}

	if info.BiState {
		shared, err := session.GetSharedState()
		keep(wrapStateErr(err, "shared state", ""))
		if err == nil && shared != nil {
			summary.SharedState = json.RawMessage(*shared)
		}
	}

	return summary, firstErr
}

func wrapStateErr(err error, kind, partition string) error {
	if err == nil {
		return nil
	}
	if partition != "" {
		return fmt.Errorf("reading %s for partition %q: %w", kind, partition, err)
	}
	return fmt.Errorf("reading %s: %w", kind, err)
}

// DescribeSource classifies the projection's source into a map with
// the type and, for categories/streams, the actual values.
func DescribeSource(info gafferruntime.ProjectionInfo) map[string]any {
	if info.AllStreams {
		return map[string]any{"type": "all"}
	}
	if len(info.Categories) > 0 {
		return map[string]any{"type": "categories", "categories": info.Categories}
	}
	if len(info.Streams) > 0 {
		return map[string]any{"type": "streams", "streams": info.Streams}
	}
	return map[string]any{"type": "unknown"}
}

// DescribePartitioning returns the projection's partitioning strategy.
func DescribePartitioning(info gafferruntime.ProjectionInfo) string {
	if info.ByStreams {
		return "byStream"
	}
	if info.ByCustomPartitions {
		return "byCustomKey"
	}
	return "none"
}

func (s StateSummary) ToMap() map[string]any {
	result := map[string]any{}

	if s.Partitioned {
		partitions := map[string]any{}
		for name, ps := range s.Partitions {
			pd := map[string]any{}
			if len(ps.State) > 0 {
				pd["state"] = json.RawMessage(ps.State)
			}
			if s.HasTransforms && len(ps.Result) > 0 {
				pd["result"] = json.RawMessage(ps.Result)
			}
			partitions[name] = pd
		}
		result["partitions"] = partitions
	} else {
		if len(s.State) > 0 {
			result["state"] = json.RawMessage(s.State)
		}
		if s.HasTransforms && len(s.Result) > 0 {
			result["result"] = json.RawMessage(s.Result)
		}
	}

	if s.HasBiState && len(s.SharedState) > 0 {
		result["sharedState"] = json.RawMessage(s.SharedState)
	}

	return result
}
