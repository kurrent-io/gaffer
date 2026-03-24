package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

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

var devEvents string

func init() {
	devCmd.Flags().StringVar(&devEvents, "events", "", "Path to JSON events file")
}

type projectionInfo struct {
	AllStreams         bool     `json:"AllStreams"`
	ByStreams          bool     `json:"ByStreams"`
	ByCustomPartitions bool     `json:"ByCustomPartitions"`
	IsBiState          bool     `json:"IsBiState"`
	Categories         []string `json:"Categories"`
	Streams            []string `json:"Streams"`
	Events             []string `json:"Events"`
}

func runDev(cmd *cobra.Command, args []string) error {
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
		return fmt.Errorf("failed to create projection session: %w", err)
	}
	defer session.Destroy()

	info := getProjectionInfo(session)
	printProjectionInfo(name, info)

	if devEvents == "" {
		return fmt.Errorf("--events flag is required (KurrentDB connection not yet supported)")
	}

	events, err := loadEvents(devEvents)
	if err != nil {
		return err
	}

	fmt.Printf("\nProcessing %d events\n\n", len(events))

	partitions := make(map[string]bool)
	for i, evt := range events {
		result, feedErr := session.Feed(evt)
		if feedErr != nil {
			return fmt.Errorf("event %d: %w", i+1, feedErr)
		}

		var parsed map[string]any
		if err := json.Unmarshal([]byte(evt), &parsed); err != nil {
			fmt.Printf("  [%d] <unparseable event>\n", i+1)
			continue
		}

		eventType, _ := parsed["eventType"].(string)
		streamID, _ := parsed["streamId"].(string)

		if result.Status == "skipped" {
			fmt.Printf("  [%d] %s @ %s (skipped: %s)\n", i+1, eventType, streamID, result.SkipReason)
		} else {
			fmt.Printf("  [%d] %s @ %s\n", i+1, eventType, streamID)
			printStepEmitted(result)
			printStepLogs(result)
			if result.Partition != "" {
				partitions[result.Partition] = true
			}
		}
	}

	fmt.Println()
	printState(session, info, partitions)

	return nil
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

func printProjectionInfo(name string, info projectionInfo) {
	fmt.Printf("Projection: %s\n", name)

	if info.AllStreams {
		fmt.Print("  Source: all streams\n")
	} else if len(info.Categories) > 0 {
		fmt.Printf("  Source: category %v\n", info.Categories)
	} else if len(info.Streams) > 0 {
		fmt.Printf("  Source: streams %v\n", info.Streams)
	}

	if info.ByStreams {
		fmt.Print("  Partitioned: per stream\n")
	} else if info.ByCustomPartitions {
		fmt.Print("  Partitioned: custom key\n")
	}

	if info.IsBiState {
		fmt.Print("  BiState: yes\n")
	}

	if len(info.Events) > 0 {
		fmt.Printf("  Events: %v\n", info.Events)
	}
}

func printStepEmitted(result *gafferruntime.FeedResult) {
	for _, e := range result.Emitted {
		if e.IsLink {
			fmt.Printf("       -> linkTo %s\n", e.StreamID)
		} else {
			fmt.Printf("       -> emit %s/%s\n", e.StreamID, e.EventType)
		}
	}
}

func printStepLogs(result *gafferruntime.FeedResult) {
	for _, msg := range result.Logs {
		fmt.Printf("       log: %s\n", msg)
	}
}

func printState(session *gafferruntime.Session, info projectionInfo, partitions map[string]bool) {
	isPartitioned := info.ByStreams || info.ByCustomPartitions

	if !isPartitioned {
		state := session.GetState(nil)
		if state != nil {
			fmt.Printf("State: %s\n", *state)
		}
	} else {
		for partition := range partitions {
			state := session.GetState(&partition)
			if state != nil {
				fmt.Printf("State [%s]: %s\n", partition, *state)
			}
		}
	}

	if info.IsBiState {
		shared := session.GetSharedState()
		if shared != nil {
			fmt.Printf("Shared state: %s\n", *shared)
		}
	}
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
