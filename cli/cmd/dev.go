package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/charmbracelet/lipgloss"
	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/project"
	"github.com/spf13/cobra"
)

var devCmd = &cobra.Command{
	Use:   "dev [projection]",
	Short: "Run a projection locally",
	Args:  cobra.ExactArgs(1),
	RunE:  runDev,
}

var (
	devEvents string
	devJSON   bool
)

func init() {
	devCmd.Flags().StringVar(&devEvents, "events", "", "Path to JSON events file")
	devCmd.Flags().BoolVar(&devJSON, "json", false, "Output as NDJSON")
}

type projectionInfo struct {
	AllStreams            bool     `json:"AllStreams"`
	ByStreams             bool     `json:"ByStreams"`
	ByCustomPartitions    bool     `json:"ByCustomPartitions"`
	IsBiState             bool     `json:"IsBiState"`
	DefinesStateTransform bool     `json:"DefinesStateTransform"`
	Categories            []string `json:"Categories"`
	Streams               []string `json:"Streams"`
	Events                []string `json:"Events"`
}

func runDev(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	name := args[0]

	root := project.FindRoot()
	if root == "" {
		return fmt.Errorf("not in a gaffer project (no gaffer.toml found)")
	}

	cfg, err := config.Load(filepath.Join(root, "gaffer.toml"))
	if err != nil {
		return err
	}

	proj := cfg.FindProjection(name)
	if proj == nil {
		return fmt.Errorf("projection %q not found in gaffer.toml", name)
	}

	source, err := os.ReadFile(filepath.Join(root, proj.Entry))
	if err != nil {
		return fmt.Errorf("reading projection source: %w", err)
	}

	session, err := gafferruntime.NewSession(string(source), nil)
	if err != nil {
		if projErr, ok := err.(gafferruntime.ProjectionError); ok {
			r := lipgloss.NewRenderer(os.Stderr)
			errStyle := r.NewStyle().Foreground(lipgloss.Color("9"))
			_, _ = fmt.Fprintf(os.Stderr, "\n%s\n%s\n", errStyle.Render(projErr.ErrorCode()), projErr.Error())
			cmd.SilenceErrors = true
			return err
		}
		return fmt.Errorf("failed to create projection session: %w", err)
	}
	defer session.Destroy()

	info := getProjectionInfo(session)

	version := proj.Engine
	if version == "" {
		version = "v2"
	}

	var writer outputWriter
	if devJSON {
		writer = newJSONWriter(os.Stdout)
	} else {
		writer = newTextWriter(os.Stdout)
	}

	writer.WriteInfo(name, info, version)

	if devEvents == "" {
		return fmt.Errorf("--events flag is required (KurrentDB connection not yet supported)")
	}

	events, err := loadEvents(devEvents)
	if err != nil {
		return err
	}

	stats, partitions, faulted := processEvents(session, events, writer)

	summary := buildSummary(session, info, partitions)
	writer.WriteSummary(stats, summary)

	if faulted {
		cmd.SilenceErrors = true
		return fmt.Errorf("projection faulted")
	}

	return nil
}

func processEvents(session *gafferruntime.Session, events []string, writer outputWriter) (eventStats, map[string]bool, bool) {
	var stats eventStats
	partitions := make(map[string]bool)

	for _, evt := range events {
		event := parseEventInfo(evt)
		writer.WriteEvent(event)

		result, feedErr := session.Feed(evt)
		if feedErr != nil {
			code, desc := classifyError(feedErr)
			writer.WriteError(event.id(), code, desc)
			stats.errors++
			return stats, partitions, true
		}

		writer.WriteResult(event.id(), result)

		if result.Status == "skipped" {
			stats.skipped++
		} else {
			stats.handled++
			if result.Partition != "" {
				partitions[result.Partition] = true
			}
		}
	}

	return stats, partitions, false
}

func classifyError(err error) (code, description string) {
	if projErr, ok := err.(gafferruntime.ProjectionError); ok {
		return projErr.ErrorCode(), projErr.ErrorDescription()
	}
	return "unexpected-error", err.Error()
}

func getProjectionInfo(session *gafferruntime.Session) projectionInfo {
	sourcesJSON := session.GetSources()
	if sourcesJSON == nil {
		return projectionInfo{}
	}

	var info projectionInfo
	if err := json.Unmarshal([]byte(*sourcesJSON), &info); err != nil {
		return projectionInfo{}
	}

	return info
}

func buildSummary(session *gafferruntime.Session, info projectionInfo, partitions map[string]bool) summaryState {
	isPartitioned := info.ByStreams || info.ByCustomPartitions

	summary := summaryState{
		partitioned:   isPartitioned,
		hasTransforms: info.DefinesStateTransform,
		hasBiState:    info.IsBiState,
	}

	if isPartitioned {
		summary.partitions = make(map[string]partitionData)
		for partition := range partitions {
			data := partitionData{}
			if state := session.GetState(&partition); state != nil {
				data.state = json.RawMessage(*state)
			}
			if info.DefinesStateTransform {
				if result, err := session.GetResult(&partition); err == nil && result != nil {
					data.result = json.RawMessage(*result)
				}
			}
			summary.partitions[partition] = data
		}
	} else {
		if state := session.GetState(nil); state != nil {
			summary.state = json.RawMessage(*state)
		}
		if info.DefinesStateTransform {
			if result, err := session.GetResult(nil); err == nil && result != nil {
				summary.result = json.RawMessage(*result)
			}
		}
	}

	if info.IsBiState {
		if shared := session.GetSharedState(); shared != nil {
			summary.sharedState = json.RawMessage(*shared)
		}
	}

	return summary
}

const zeroUUID = "00000000-0000-0000-0000-000000000000"

func loadEvents(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading events file: %w", err)
	}

	var events []json.RawMessage
	if err := json.Unmarshal(data, &events); err != nil {
		return nil, fmt.Errorf("parsing events file (expected JSON array): %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	result := make([]string, len(events))
	for i, evt := range events {
		var obj map[string]any
		if err := json.Unmarshal(evt, &obj); err != nil {
			return nil, fmt.Errorf("event %d: %w", i+1, err)
		}

		if _, ok := obj["sequenceNumber"]; !ok {
			obj["sequenceNumber"] = i
		}
		if _, ok := obj["isJson"]; !ok {
			obj["isJson"] = true
		}
		if _, ok := obj["eventId"]; !ok {
			obj["eventId"] = zeroUUID
		}
		if _, ok := obj["created"]; !ok {
			obj["created"] = now
		}

		normalized, err := json.Marshal(obj)
		if err != nil {
			return nil, fmt.Errorf("event %d: %w", i+1, err)
		}
		result[i] = string(normalized)
	}

	return result, nil
}
