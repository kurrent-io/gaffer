package cmd

import (
	"context"
	"encoding/json"
	"io"

	"github.com/charmbracelet/lipgloss"

	"github.com/kurrent-io/gaffer/cli/internal/cliout"
	"github.com/kurrent-io/gaffer/cli/internal/drift"
)

// deploySink consumes the deploy run's progress. start fires before a
// projection's RPC (the interactive sink uses it to spin the active line);
// done fires with its outcome; finish flushes (the JSON sink emits its array,
// the interactive sink tears down its program). The three sinks - JSON, plain
// streaming, and interactive - render the same event stream three ways.
type deploySink interface {
	start(name string, index, total int)
	done(res drift.Result)
	finish() error
}

// newDeploySink picks the renderer: machine output when --json (a single final
// array, or NDJSON progress when stream), otherwise the interactive program on a
// terminal and plain streaming lines off one (pipes, CI, tests) - the same
// terminal gate that drives coloured output. cancel stops the deploy when the
// interactive view is interrupted; the non-interactive sinks don't need it (a
// pipe's Ctrl-C arrives as a signal the command context handles).
func newDeploySink(w, errW io.Writer, jsonOut, stream bool, names []string, ctx context.Context, cancel context.CancelFunc) deploySink {
	if jsonOut {
		if stream {
			return &streamSink{enc: json.NewEncoder(w)}
		}
		return &jsonSink{w: w, results: []cliout.DeployJSON{}}
	}
	if interactiveWriter(w) {
		return newTeaSink(w, names, ctx, cancel)
	}
	return newPlainSink(w, errW, names)
}

// maxNameWidth is the display-cell width the name is padded to so verdicts
// align, known up front because every name is resolved before the run starts.
func maxNameWidth(names []string) int {
	w := 0
	for _, n := range names {
		if cw := lipgloss.Width(n); cw > w {
			w = cw
		}
	}
	return w
}

type jsonSink struct {
	w       io.Writer
	results []cliout.DeployJSON
}

func (s *jsonSink) start(string, int, int) {}

func (s *jsonSink) done(res drift.Result) {
	s.results = append(s.results, cliout.BuildDeployJSON(res))
}

func (s *jsonSink) finish() error {
	enc := json.NewEncoder(s.w)
	enc.SetIndent("", "  ")
	return enc.Encode(s.results)
}

// streamStartMsg / streamResultMsg / streamSummaryMsg are the NDJSON events the
// stream sink emits: one object per line, each type-tagged so it slots into the
// editor's shared CLI message stream (the deploy webview consumes them live).
// Distinct deploy_* types because the run path already owns "result"/"summary"
// with other shapes.
type streamStartMsg struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Index int    `json:"index"`
	Total int    `json:"total"`
}

type streamResultMsg struct {
	Type              string `json:"type"`
	cliout.DeployJSON        // flattened onto the message: name, outcome, flags, error
}

type streamSummaryMsg struct {
	Type    string `json:"type"`
	Created int    `json:"created"`
	Updated int    `json:"updated"`
	Rebuilt int    `json:"rebuilt"`
	Skipped int    `json:"skipped"`
	Refused int    `json:"refused"`
	Invalid int    `json:"invalid"`
	Failed  int    `json:"failed"`
}

// streamSink emits NDJSON progress as the apply runs (deploy --json --stream): a
// deploy_start as each projection's RPC begins, a deploy_result as it settles,
// and a terminal deploy_summary. Unlike jsonSink it flushes per event so a
// consumer renders progress live instead of waiting for the whole run. A write
// failure (a consumer disconnecting mid-run: a closed webview, a broken pipe)
// never aborts the in-flight deploy - emit goes quiet after the first error and
// finish surfaces it, so the apply always runs to completion.
type streamSink struct {
	enc      *json.Encoder
	counts   deployCounts
	writeErr error
}

func (s *streamSink) emit(v any) {
	if s.writeErr == nil {
		s.writeErr = s.enc.Encode(v)
	}
}

func (s *streamSink) start(name string, index, total int) {
	s.emit(streamStartMsg{Type: "deploy_start", Name: name, Index: index, Total: total})
}

func (s *streamSink) done(res drift.Result) {
	s.counts.add(res)
	s.emit(streamResultMsg{Type: "deploy_result", DeployJSON: cliout.BuildDeployJSON(res)})
}

func (s *streamSink) finish() error {
	s.emit(streamSummaryMsg{
		Type:    "deploy_summary",
		Created: s.counts.created,
		Updated: s.counts.updated,
		Rebuilt: s.counts.rebuilt,
		Skipped: s.counts.skipped,
		Refused: s.counts.refused,
		Invalid: s.counts.invalid,
		Failed:  s.counts.failed,
	})
	return s.writeErr
}

// emptyStreamSummary emits the terminal deploy_summary for a --json --stream run
// with nothing to deploy, so a streaming consumer always ends on a summary line.
func emptyStreamSummary(w io.Writer) error {
	return (&streamSink{enc: json.NewEncoder(w)}).finish()
}

// plainSink streams one verdict line per projection as it completes, then a
// summary. No animation: start is a no-op, the line is printed on done. Matches
// the dev/run streaming-text idiom and stays copyable in pipes and CI logs.
type plainSink struct {
	tw        *textWriter
	nameWidth int
	counts    deployCounts
}

func newPlainSink(w, errW io.Writer, names []string) *plainSink {
	return &plainSink{tw: newTextWriter(w, errW), nameWidth: maxNameWidth(names)}
}

func (s *plainSink) start(string, int, int) {}

func (s *plainSink) done(res drift.Result) {
	s.counts.add(res)
	s.tw.write("%s\n", s.tw.deployResultLine(res, s.nameWidth))
}

func (s *plainSink) finish() error {
	s.tw.writeDeploySummary(s.counts)
	return nil
}
