package lsp

import (
	"context"
	"strings"
	"testing"

	"github.com/sourcegraph/jsonrpc2"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/drift"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

// named builds an in-config status entry for a projection with an explicit
// name and optional runtime state (StateUnknown == not deployed, Runtime nil).
func named(name string, state drift.State, rt remote.State) drift.StatusEntry {
	e := drift.StatusEntry{Comparison: drift.Comparison{Name: name, State: state}}
	if rt != remote.StateUnknown {
		e.Runtime = &remote.Status{State: rt}
	}
	return e
}

func TestEntryHealth(t *testing.T) {
	for _, tc := range []struct {
		name  string
		entry drift.StatusEntry
		want  projHealth
	}{
		{"in sync + running is green", named("p", drift.InSync, remote.StateRunning), healthGreen},
		{"in sync + stopped is still green", named("p", drift.InSync, remote.StateStopped), healthGreen},
		{"in sync + aborted is green (runtime shows in the state column, not the dot)", named("p", drift.InSync, remote.StateAborted), healthGreen},
		{"faulted is red even when in sync", named("p", drift.InSync, remote.StateFaulted), healthRed},
		{"invalid is red", named("p", drift.Invalid, remote.StateUnknown), healthRed},
		{"drifted is orange", named("p", drift.Drifted, remote.StateRunning), healthOrange},
		{"not deployed is orange", named("p", drift.NotDeployed, remote.StateUnknown), healthOrange},
		{"faulted wins over drifted", named("p", drift.Drifted, remote.StateFaulted), healthRed},
	} {
		if got := entryHealth(tc.entry); got != tc.want {
			t.Errorf("%s: entryHealth = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestProjectionEnvCells_Markers(t *testing.T) {
	// Each env's badge marker distinguishes the reason for a missing reading:
	// locked (sign-in), error (fetch failed), loading (not yet cached).
	desc := config.Description{Environments: []config.EnvDescription{
		{Name: "synced"}, {Name: "locked"}, {Name: "failed"}, {Name: "pending"},
	}}
	statuses := map[string]envStatus{
		"synced": {Entries: []drift.StatusEntry{named("p", drift.InSync, remote.StateRunning)}},
		"locked": {Unauthenticated: true},
		"failed": {Err: errStub{}},
		// "pending" absent from the cache -> loading.
	}
	got := map[string]string{}
	for _, c := range projectionEnvCells(desc, "p", statuses) {
		got[c.Env] = c.Marker
	}
	want := map[string]string{
		"synced":  "green",
		"locked":  "locked",
		"failed":  "error",
		"pending": "loading",
	}
	for env, w := range want {
		if got[env] != w {
			t.Errorf("%s marker: got %q want %q", env, got[env], w)
		}
	}
}

func TestProjectionEnvCells(t *testing.T) {
	desc := config.Description{Environments: []config.EnvDescription{
		{Name: "prod"}, {Name: "staging"}, {Name: "dev"}, {Name: "qa"},
	}}
	statuses := map[string]envStatus{
		"prod":    {Entries: []drift.StatusEntry{named("checkout", drift.InSync, remote.StateRunning)}},
		"staging": {Unauthenticated: true},
		"dev":     {Err: errStub{}},
		// qa: not cached at all
	}
	cells := projectionEnvCells(desc, "checkout", statuses)
	if len(cells) != 4 {
		t.Fatalf("expected one cell per env, got %d", len(cells))
	}
	// Order follows the config's env order, not map iteration.
	if cells[0].Env != "prod" || cells[3].Env != "qa" {
		t.Fatalf("cells out of order: %+v", cells)
	}
	if !cells[0].Known || cells[0].Health != healthGreen || cells[0].State != "running" || cells[0].Verdict != drift.LabelInSync {
		t.Errorf("prod cell: %+v", cells[0])
	}
	if cells[1].Known || cells[1].Note != "sign-in needed" {
		t.Errorf("staging cell: %+v", cells[1])
	}
	if cells[2].Known || cells[2].Note != "status unavailable" {
		t.Errorf("dev cell: %+v", cells[2])
	}
	if cells[3].Known || cells[3].Note != "checking…" {
		t.Errorf("qa cell: %+v", cells[3])
	}
}

func TestProjectionEnvCells_ProjectionNotInEntries(t *testing.T) {
	desc := config.Description{Environments: []config.EnvDescription{{Name: "prod"}}}
	statuses := map[string]envStatus{
		"prod": {Entries: []drift.StatusEntry{named("other", drift.InSync, remote.StateRunning)}},
	}
	cells := projectionEnvCells(desc, "checkout", statuses)
	if len(cells) != 1 || cells[0].Known || cells[0].Note != "no status" {
		t.Errorf("a projection absent from a clean fetch should degrade to 'no status': %+v", cells)
	}
}

func TestProjectionEnvCells_FileOrder(t *testing.T) {
	// "alpha" sorts first by name but is declared later in the file; cells must
	// follow file order (by header line), and an unlocated env sorts last.
	desc := config.Description{Environments: []config.EnvDescription{
		{Name: "zeta", Range: config.SourceRange{StartLine: 3, EndLine: 3}},
		{Name: "alpha", Range: config.SourceRange{StartLine: 9, EndLine: 9}},
		{Name: "quoted"}, // no located header
	}}
	st := envStatus{Entries: []drift.StatusEntry{named("p", drift.InSync, remote.StateRunning)}}
	statuses := map[string]envStatus{"zeta": st, "alpha": st, "quoted": st}
	cells := projectionEnvCells(desc, "p", statuses)
	if len(cells) != 3 || cells[0].Env != "zeta" || cells[1].Env != "alpha" || cells[2].Env != "quoted" {
		t.Errorf("env order: got [%s %s %s] want [zeta alpha quoted]", cells[0].Env, cells[1].Env, cells[2].Env)
	}
}

func TestMarkerDotSVG(t *testing.T) {
	// Each known health fills with its palette color; a recolor must break here.
	for marker, want := range map[string]string{"green": "#3fb950", "orange": "#d29922", "red": "#f85149"} {
		if !strings.Contains(markerDotSVG(marker), want) {
			t.Errorf("%s dot should be filled with %s: %s", marker, want, markerDotSVG(marker))
		}
	}
	locked := markerDotSVG("locked")
	if !strings.Contains(locked, `fill="none"`) || strings.Contains(locked, "<line") {
		t.Errorf("locked should be a hollow ring, no slash: %s", locked)
	}
	errDot := markerDotSVG("error")
	if !strings.Contains(errDot, `fill="none"`) || !strings.Contains(errDot, "<line") {
		t.Errorf("error should be a ring with a slash: %s", errDot)
	}
	if !strings.Contains(markerDotSVG("loading"), "opacity") {
		t.Error("loading should be a faint dot")
	}
	// An unknown marker degrades to a plain ring, not a crash or empty svg.
	unknown := markerDotSVG("bogus")
	if !strings.Contains(unknown, `fill="none"`) || strings.Contains(unknown, "<line") {
		t.Errorf("an unknown marker should degrade to a hollow ring: %s", unknown)
	}
}

func TestProjectionHoverMarkdown_SanitizesEnvName(t *testing.T) {
	// A quoted env key can carry a backtick or newline; both must be stripped so
	// they can't break out of the code span or the line.
	const weird = "pr`od\nx"
	desc := config.Description{Environments: []config.EnvDescription{{Name: weird}}}
	statuses := map[string]envStatus{
		weird: {Entries: []drift.StatusEntry{named("p", drift.InSync, remote.StateRunning)}},
	}
	md := projectionHoverMarkdown(desc, config.ProjectionDescription{Name: "p"}, statuses)
	if !strings.Contains(md, "`prod x`") {
		t.Errorf("env name should render as a single unbroken code span: %s", md)
	}
	if strings.Contains(md, "`pr`") || strings.Contains(md, "\n") {
		t.Errorf("backtick and newline must not survive into the markdown: %q", md)
	}
}

func TestProjectionHoverMarkdown(t *testing.T) {
	desc := config.Description{Environments: []config.EnvDescription{
		{Name: "prod"}, {Name: "staging"},
	}}
	statuses := map[string]envStatus{
		"prod":    {Entries: []drift.StatusEntry{named("checkout", drift.InSync, remote.StateRunning)}},
		"staging": {Entries: []drift.StatusEntry{named("checkout", drift.Drifted, remote.StateFaulted)}},
	}
	md := projectionHoverMarkdown(desc, config.ProjectionDescription{Name: "checkout"}, statuses)

	for _, want := range []string{
		"`prod` · `in sync` · `running`",
		"`staging` · `drifted` · `faulted`",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("hover markdown missing %q:\n%s", want, md)
		}
	}
	if !strings.Contains(md, "![](data:image/svg+xml;base64,") {
		t.Errorf("dots should render as embedded SVG images:\n%s", md)
	}
	if strings.Contains(md, "| Env |") {
		t.Errorf("hover should be a borderless list, not a table:\n%s", md)
	}
}

func TestProjectionHoverMarkdown_UnknownCellRendersNote(t *testing.T) {
	desc := config.Description{Environments: []config.EnvDescription{{Name: "prod"}}}
	statuses := map[string]envStatus{"prod": {Unauthenticated: true}}
	md := projectionHoverMarkdown(desc, config.ProjectionDescription{Name: "p"}, statuses)
	if !strings.Contains(md, "`prod` · `sign-in needed`") {
		t.Errorf("an unknown env should show its note in place of a verdict:\n%s", md)
	}
	if strings.Contains(md, "`—`") {
		t.Errorf("an unknown env should omit the state field, not show a dash:\n%s", md)
	}
}

func TestProjectionHoverMarkdown_NoEnvsIsEmpty(t *testing.T) {
	desc := config.Description{Projections: []config.ProjectionDescription{{Name: "p"}}}
	if md := projectionHoverMarkdown(desc, config.ProjectionDescription{Name: "p"}, nil); md != "" {
		t.Errorf("no configured envs should render no hover, got %q", md)
	}
}

func TestProjectionHoverMarkdown_NotDeployedOmitsState(t *testing.T) {
	desc := config.Description{Environments: []config.EnvDescription{{Name: "prod"}}}
	statuses := map[string]envStatus{
		"prod": {Entries: []drift.StatusEntry{named("p", drift.NotDeployed, remote.StateUnknown)}},
	}
	md := projectionHoverMarkdown(desc, config.ProjectionDescription{Name: "p"}, statuses)
	if !strings.Contains(md, "`prod` · `not deployed`") {
		t.Errorf("not-deployed row should show env and verdict:\n%s", md)
	}
	// No runtime state exists, so there's no third field after the verdict.
	if strings.Contains(md, "`not deployed` · ") {
		t.Errorf("not-deployed row should omit the state field:\n%s", md)
	}
}

func TestProjectionAt(t *testing.T) {
	desc := config.Description{Projections: []config.ProjectionDescription{
		{Name: "a", Range: config.SourceRange{StartLine: 5, EndLine: 5}},
		{Name: "b", Range: config.SourceRange{StartLine: 9, EndLine: 9}},
		{Name: "bad", Range: config.SourceRange{StartLine: 12, EndLine: 12}, Diagnostic: &config.Diagnostic{Message: "x"}},
		{Name: "unlocated"}, // zero range - must not claim line 0
	}}
	// Source line 5 is 0-indexed LSP line 4.
	if p, ok := projectionAt(desc, 4); !ok || p.Name != "a" {
		t.Errorf("line 4 should resolve to a, got %q ok=%v", p.Name, ok)
	}
	if p, ok := projectionAt(desc, 8); !ok || p.Name != "b" {
		t.Errorf("line 8 should resolve to b, got %q ok=%v", p.Name, ok)
	}
	if _, ok := projectionAt(desc, 3); ok {
		t.Error("a non-header line should resolve to no projection")
	}
	if _, ok := projectionAt(desc, 11); ok {
		t.Error("a diagnostic-bearing projection should be skipped")
	}
	if _, ok := projectionAt(desc, 0); ok {
		t.Error("an unlocated (zero-range) projection must not claim line 0")
	}
}

const hoverConfig = `[env.local]
connection = "esdb://localhost:2113"
default = true

[[projection]]
name = "checkout"
entry = "checkout.js"
engine_version = 2
`

// seedHoverServer opens and parses hoverConfig so GetParse is populated, and
// returns the server plus the config URI. statusLens is enabled by testServer.
func seedHoverServer(t *testing.T, fetch statusFetchFunc) (*Server, string) {
	t.Helper()
	root := t.TempDir()
	cfg := writeWorkspaceFile(t, root, "gaffer.toml", hoverConfig)
	writeWorkspaceFile(t, root, "checkout.js", "function project(){}")
	uri := pathToURI(cfg)
	s := testServer(fetch)
	s.docs.Open(uri, hoverConfig)
	s.parseAndPublish(context.Background(), uri)
	if _, ok := s.docs.GetParse(uri); !ok {
		t.Fatal("precondition: parse should be cached")
	}
	return s, uri
}

func hoverReq(t *testing.T, uri string, line int) *jsonrpc2.Request {
	t.Helper()
	req := &jsonrpc2.Request{}
	if err := req.SetParams(HoverParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: line},
	}); err != nil {
		t.Fatal(err)
	}
	return req
}

func TestHandleHover_OnProjectionHeader(t *testing.T) {
	s, uri := seedHoverServer(t, nil)
	s.statusCache.store(uri, "local", 0, envStatus{
		Entries: []drift.StatusEntry{named("checkout", drift.InSync, remote.StateRunning)},
	})

	// [[projection]] is on 0-indexed line 4 of hoverConfig.
	got, err := s.handleHover(hoverReq(t, uri, 4))
	if err != nil {
		t.Fatal(err)
	}
	h, ok := got.(Hover)
	if !ok {
		t.Fatalf("expected a Hover, got %T (%v)", got, got)
	}
	if h.Contents.Kind != MarkupKindMarkdown {
		t.Errorf("kind: %q", h.Contents.Kind)
	}
	if !strings.Contains(h.Contents.Value, "`local` · `in sync` · `running`") {
		t.Errorf("hover body:\n%s", h.Contents.Value)
	}
	if h.Range == nil || h.Range.Start.Line != 4 {
		t.Errorf("hover range should anchor the header line 4, got %+v", h.Range)
	}
}

func TestHandleHover_OffHeaderIsNil(t *testing.T) {
	s, uri := seedHoverServer(t, nil)
	s.statusCache.store(uri, "local", 0, envStatus{
		Entries: []drift.StatusEntry{named("checkout", drift.InSync, remote.StateRunning)},
	})
	// Line 1 is the connection line, not a projection header.
	got, err := s.handleHover(hoverReq(t, uri, 1))
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("expected no hover off a header, got %v", got)
	}
}

func TestHandleHover_NilParamsIsNil(t *testing.T) {
	s, _ := seedHoverServer(t, nil)
	got, err := s.handleHover(&jsonrpc2.Request{})
	if err != nil || got != nil {
		t.Errorf("nil params should yield no hover and no error, got %v / %v", got, err)
	}
}

func TestHandleHover_NonConfigURIIsNil(t *testing.T) {
	s, _ := seedHoverServer(t, nil)
	got, err := s.handleHover(hoverReq(t, "file:///ws/checkout.js", 0))
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("a non-gaffer.toml URI should yield no hover, got %v", got)
	}
}

func TestHandleHover_NoParseIsNil(t *testing.T) {
	s := testServer(nil)
	// A gaffer.toml URI the server never parsed.
	got, err := s.handleHover(hoverReq(t, "file:///ws/gaffer.toml", 4))
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("a config with no cached parse should yield no hover, got %v", got)
	}
}

func TestHandleHover_NoEnvsIsNil(t *testing.T) {
	const noEnvConfig = `[[projection]]
name = "checkout"
entry = "checkout.js"
engine_version = 2
`
	root := t.TempDir()
	cfg := writeWorkspaceFile(t, root, "gaffer.toml", noEnvConfig)
	writeWorkspaceFile(t, root, "checkout.js", "function project(){}")
	uri := pathToURI(cfg)
	s := testServer(nil)
	s.docs.Open(uri, noEnvConfig)
	s.parseAndPublish(context.Background(), uri)

	// [[projection]] is on line 0; with no configured envs there's nothing to show.
	got, err := s.handleHover(hoverReq(t, uri, 0))
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("a projection with no configured envs should yield no hover, got %v", got)
	}
}

func TestHandleHover_NotOptedInIsNil(t *testing.T) {
	s, uri := seedHoverServer(t, nil)
	s.statusLensCapable = false
	got, err := s.handleHover(hoverReq(t, uri, 4))
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("a client that didn't opt in should get no hover, got %v", got)
	}
}

func TestHandleInitialize_HoverGatedOnStatusLens(t *testing.T) {
	// Opted in: hover is advertised.
	on := NewServer(ServerOptions{})
	reqOn := &jsonrpc2.Request{}
	if err := reqOn.SetParams(InitializeParams{InitOptions: []byte(`{"statusLens":true}`)}); err != nil {
		t.Fatal(err)
	}
	res, err := on.handleInitialize(context.Background(), reqOn)
	if err != nil {
		t.Fatal(err)
	}
	resOn, ok := res.(InitializeResult)
	if !ok {
		t.Fatalf("initialize returned %T", res)
	}
	if resOn.Capabilities.HoverProvider == nil {
		t.Error("hover should be advertised when the client opts into statusLens")
	}

	// Not opted in: no hover capability, so the client keeps its own hover.
	off := NewServer(ServerOptions{})
	res, err = off.handleInitialize(context.Background(), &jsonrpc2.Request{})
	if err != nil {
		t.Fatal(err)
	}
	resOff, ok := res.(InitializeResult)
	if !ok {
		t.Fatalf("initialize returned %T", res)
	}
	if resOff.Capabilities.HoverProvider != nil {
		t.Error("hover should not be advertised without the statusLens opt-in")
	}
}
