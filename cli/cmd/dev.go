package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

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
	devDebug  bool
)

func init() {
	devCmd.Flags().StringVar(&devEvents, "events", "", "Path to JSON events file")
	devCmd.Flags().BoolVar(&devDebug, "debug", false, "Enable debug mode")
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

	session := gafferruntime.SessionCreate(string(source), nil)
	if session == nil {
		return fmt.Errorf("failed to create projection session (check JS syntax)")
	}
	defer gafferruntime.SessionDestroy(session)

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
		result := gafferruntime.SessionFeed(session, evt)
		if result != 0 {
			errMsg := gafferruntime.SessionGetError(session)
			if errMsg != nil {
				return fmt.Errorf("event %d: %s", i+1, *errMsg)
			}
			return fmt.Errorf("event %d: unknown error", i+1)
		}

		var parsed map[string]any
		if err := json.Unmarshal([]byte(evt), &parsed); err == nil {
			eventType, _ := parsed["eventType"].(string)
			streamID, _ := parsed["streamId"].(string)
			fmt.Printf("  [%d] %s @ %s\n", i+1, eventType, streamID)
			if streamID != "" {
				partitions[streamID] = true
			}
		}
	}

	fmt.Println()
	printState(session, info, partitions)

	return nil
}

func getProjectionInfo(session *gafferruntime.Session) projectionInfo {
	sourcesJSON := gafferruntime.SessionGetSources(session)
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

func printState(session *gafferruntime.Session, info projectionInfo, partitions map[string]bool) {
	isPartitioned := info.ByStreams || info.ByCustomPartitions

	if !isPartitioned {
		state := gafferruntime.SessionGetState(session, nil)
		if state != nil {
			fmt.Printf("State: %s\n", *state)
		}
	} else {
		for partition := range partitions {
			pState := gafferruntime.SessionGetState(session, &partition)
			if pState != nil {
				fmt.Printf("State [%s]: %s\n", partition, *pState)
			}
		}
	}

	if info.IsBiState {
		shared := gafferruntime.SessionGetSharedState(session)
		if shared != nil {
			fmt.Printf("Shared state: %s\n", *shared)
		}
	}
}

func loadEvents(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading events file: %w", err)
	}

	var events []json.RawMessage
	if err := json.Unmarshal(data, &events); err != nil {
		return nil, fmt.Errorf("parsing events file (expected JSON array): %w", err)
	}

	result := make([]string, len(events))
	for i, evt := range events {
		result[i] = string(evt)
	}

	return result, nil
}
