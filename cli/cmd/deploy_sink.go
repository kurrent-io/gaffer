package cmd

import (
	"context"
	"encoding/json"
	"io"
)

// deploySink consumes the deploy run's progress. start fires before a
// projection's RPC (the interactive sink uses it to spin the active line);
// done fires with its outcome; finish flushes (the JSON sink emits its array,
// the interactive sink tears down its program). The three sinks - JSON, plain
// streaming, and interactive - render the same event stream three ways.
type deploySink interface {
	start(name string, index, total int)
	done(res deployResult)
	finish() error
}

// newDeploySink picks the renderer: machine output when --json, otherwise the
// interactive program on a terminal and plain streaming lines off one (pipes,
// CI, tests) - the same terminal gate that drives coloured output. cancel stops
// the deploy when the interactive view is interrupted; the non-interactive sinks
// don't need it (a pipe's Ctrl-C arrives as a signal the command context handles).
func newDeploySink(w, errW io.Writer, jsonOut bool, names []string, ctx context.Context, cancel context.CancelFunc) deploySink {
	if jsonOut {
		return &jsonSink{w: w, results: []deployJSON{}}
	}
	if interactiveWriter(w) {
		return newTeaSink(w, names, ctx, cancel)
	}
	return newPlainSink(w, errW, names)
}

// maxNameWidth is the column width the name is padded to so verdicts align,
// known up front because every name is resolved before the run starts.
func maxNameWidth(names []string) int {
	w := 0
	for _, n := range names {
		if len(n) > w {
			w = len(n)
		}
	}
	return w
}

// deployJSON is the --json shape for one projection. outcome is the verdict
// (created, updated, skipped, refused, failed); reason is set for refused,
// error for failed.
type deployJSON struct {
	Name    string `json:"name"`
	Outcome string `json:"outcome"`
	Reason  string `json:"reason,omitempty"`
	Error   string `json:"error,omitempty"`
}

type jsonSink struct {
	w       io.Writer
	results []deployJSON
}

func (s *jsonSink) start(string, int, int) {}

func (s *jsonSink) done(res deployResult) {
	j := deployJSON{Name: res.Name, Outcome: res.outcome(), Reason: res.Reason}
	if res.Err != nil {
		j.Error = res.Err.Error()
	}
	s.results = append(s.results, j)
}

func (s *jsonSink) finish() error {
	enc := json.NewEncoder(s.w)
	enc.SetIndent("", "  ")
	return enc.Encode(s.results)
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

func (s *plainSink) done(res deployResult) {
	s.counts.add(res)
	s.tw.write("%s\n", s.tw.deployResultLine(res, s.nameWidth))
}

func (s *plainSink) finish() error {
	s.tw.writeDeploySummary(s.counts)
	return nil
}
