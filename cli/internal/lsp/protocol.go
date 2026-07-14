// Package lsp implements the gaffer Language Server Protocol server.
//
// Hand-written LSP message types for the narrow surface gaffer needs:
// initialize / initialized / shutdown / exit plus textDocument
// lifecycle, codeLens, publishDiagnostics, watchers (added in
// subsequent chunks).
//
// We use sourcegraph/jsonrpc2 for Content-Length framing and request
// dispatch but write our own protocol struct definitions - the LSP
// surface we use is small enough that vendoring a full protocol
// library (e.g. tliron/glsp) buys little and pulls in deps we don't
// want. See ~/notes/gaffer/editor-extensions/planning/lsp-plan.md
// Decision 7 for the rationale.
package lsp

import (
	"encoding/json"

	"github.com/kurrent-io/gaffer/cli/internal/config"
)

// LSP method names. Server registers handlers keyed on these.
const (
	MethodInitialize  = "initialize"
	MethodInitialized = "initialized"
	MethodShutdown    = "shutdown"
	MethodExit        = "exit"

	MethodDidOpen   = "textDocument/didOpen"
	MethodDidChange = "textDocument/didChange"
	MethodDidClose  = "textDocument/didClose"
	MethodDidSave   = "textDocument/didSave"

	MethodCodeLens           = "textDocument/codeLens"
	MethodPublishDiagnostics = "textDocument/publishDiagnostics"

	MethodDidChangeWatchedFiles = "workspace/didChangeWatchedFiles"
	MethodRegisterCapability    = "client/registerCapability"
	MethodWorkspaceSymbol       = "workspace/symbol"
	MethodCodeLensRefresh       = "workspace/codeLens/refresh"

	// MethodProjectionDetails is a gaffer-specific extension. Returns
	// the bits of a projection's parsed config that the editor needs
	// to drive the Run Projection picker (live vs fixture). Editors
	// without this knowledge (zed, neovim) just default to live.
	MethodProjectionDetails = "gaffer/projectionDetails"

	// MethodRefreshStatus is a gaffer-specific extension: the editor asks
	// the server to re-fetch deploy status for one gaffer.toml after an
	// out-of-band change the server can't observe, such as a sign-in
	// completing in an editor-spawned terminal. Fire-and-forget - the fresh
	// status reaches the editor through the normal codeLens refresh once it
	// lands.
	MethodRefreshStatus = "gaffer/refreshStatus"
)

// LSP intent codes for code lenses. Per the LSP plan, the server
// emits a semantic intent in `data.intent` and each editor extension
// maps it to its native icon / treatment. Five intents cover the
// surface; the server only emits two (Debug / DebugChoose) - the
// rest (stop, starting, untrusted) are client-side concerns the
// extension overrides on top of the server's lens.
const (
	IntentDebug       = "debug"
	IntentDebugChoose = "debug-choose"
	// IntentStatusEnv marks the non-clickable env-block deploy-status
	// roll-up; the client renders the server's title as informational text.
	IntentStatusEnv = "status-env"
	// IntentStatusLoading marks a placeholder shown while an env's status
	// fetch is still in flight; the client renders it with a spinner.
	IntentStatusLoading = "status-loading"
	// IntentSignIn marks the env-block sign-in action shown when an env
	// needs authentication; the client routes it to its sign-in flow.
	IntentSignIn = "sign-in"
)

// Gaffer command IDs surfaced via CodeLens.command. Each editor
// extension routes these to its native launch API.
const (
	CommandDebugProjection     = "gaffer.debugProjection"
	CommandDebugProjectionPick = "gaffer.debugProjectionPick"
	CommandSignIn              = "gaffer.signIn"
)

// InitializeParams is the subset of LSP InitializeParams we care
// about. Real LSP InitializeParams has dozens of fields; unmodelled
// fields stay in the wire JSON without forcing us to commit to a
// shape. InitOptions is held as json.RawMessage so the eventual
// consumer can decode just-in-time, preserving forward compat and
// avoiding integer-as-float64 coercion that map[string]any
// would force.
type InitializeParams struct {
	ProcessID        *int               `json:"processId,omitempty"`
	RootURI          string             `json:"rootUri,omitempty"`
	WorkspaceFolders []WorkspaceFolder  `json:"workspaceFolders,omitempty"`
	Capabilities     ClientCapabilities `json:"capabilities"`
	InitOptions      json.RawMessage    `json:"initializationOptions,omitempty"`
}

// ClientCapabilities captures the slices of the client's
// capability tree we actually gate behavior on. The full LSP
// type has dozens of nested fields; we model only what we
// consult.
type ClientCapabilities struct {
	Workspace WorkspaceClientCapabilities `json:"workspace"`
}

// WorkspaceClientCapabilities is the workspace-scoped subset of
// ClientCapabilities. CodeLens lives here in the LSP spec.
type WorkspaceClientCapabilities struct {
	CodeLens *CodeLensWorkspaceClientCapabilities `json:"codeLens,omitempty"`
}

// CodeLensWorkspaceClientCapabilities advertises whether the
// client can handle workspace/codeLens/refresh. Servers MUST
// gate that request on this flag per the LSP 3.16 spec.
type CodeLensWorkspaceClientCapabilities struct {
	RefreshSupport bool `json:"refreshSupport,omitempty"`
}

// WorkspaceFolder is an entry in InitializeParams.WorkspaceFolders.
type WorkspaceFolder struct {
	URI  string `json:"uri"`
	Name string `json:"name"`
}

// InitializeResult is what we send back to the client. ServerInfo
// helps with logs / "About Gaffer LSP" UIs in the editor.
type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
	ServerInfo   ServerInfo         `json:"serverInfo"`
}

// ServerInfo identifies the server in the client's UI / logs.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// ServerCapabilities advertises what the server provides. V1 surface
// is intentionally minimal; more fields land with chunks 2.2+.
type ServerCapabilities struct {
	// TextDocumentSync uses the options form so we can request `save`
	// notifications (Full sync since config files are tiny - LSP plan
	// Decision 1). didSave drives a deploy-status refresh directly, rather
	// than relying only on the file watcher, so clients that don't support
	// dynamic watcher registration still refresh status on save.
	TextDocumentSync TextDocumentSyncOptions `json:"textDocumentSync"`
	// CodeLensProvider advertises that the server responds to
	// textDocument/codeLens requests. Empty options struct is
	// fine - we don't require resolveProvider since lenses are
	// fully populated on the initial response.
	CodeLensProvider *CodeLensOptions `json:"codeLensProvider,omitempty"`
	// WorkspaceSymbolProvider advertises that the server responds
	// to workspace/symbol requests. Lets editors fuzzy-find
	// projections via Cmd+T and powers our QuickPick.
	WorkspaceSymbolProvider *WorkspaceSymbolOptions `json:"workspaceSymbolProvider,omitempty"`
}

// TextDocumentSyncKind matches LSP spec values: 0=None, 1=Full,
// 2=Incremental. We only emit Full today; add the others if/when
// incremental sync is needed (LSP plan Decision 1 picked Full).
type TextDocumentSyncKind int

const (
	TextDocumentSyncFull TextDocumentSyncKind = 1
)

// TextDocumentSyncOptions is the object form of the textDocumentSync
// capability. OpenClose requests didOpen/didClose (which the parse pipeline
// needs); Save requests didSave (which drives status refresh); Change is the
// sync kind. Save is the bare-bool form ("send didSave without the text") -
// the server already holds the buffer from full sync.
type TextDocumentSyncOptions struct {
	OpenClose bool                 `json:"openClose"`
	Change    TextDocumentSyncKind `json:"change"`
	Save      bool                 `json:"save"`
}

// InitializationOptions is the client-supplied initializationOptions blob the
// server consults. StatusLens is the VS Code extension's signal that it can
// render the deploy-status lenses (the informational roll-up isn't expressible
// as a routable command, so other editors don't opt in and don't receive it).
type InitializationOptions struct {
	StatusLens bool `json:"statusLens"`
}

// CodeLensOptions is the value of ServerCapabilities.CodeLensProvider.
type CodeLensOptions struct {
	// ResolveProvider would be true if the server wanted clients
	// to call back via codeLens/resolve to fill in lens details
	// lazily. We populate everything upfront so it's false/absent.
	ResolveProvider bool `json:"resolveProvider,omitempty"`
}

// WorkspaceSymbolOptions is the value of
// ServerCapabilities.WorkspaceSymbolProvider when sent as an
// object (the spec also accepts a bare bool).
type WorkspaceSymbolOptions struct{}

// WorkspaceSymbolParams is the request payload for workspace/symbol.
// Query is a fuzzy filter; we treat empty as "return everything"
// and let the client do the matching, since our domain (projection
// names) is small.
type WorkspaceSymbolParams struct {
	Query string `json:"query"`
}

// SymbolKind matches LSP spec values. We only emit Function for
// projections; the full enum has 26 entries that we don't need.
type SymbolKind int

const (
	// SymbolKindFunction is the closest fit for a projection - it's
	// a callable, named unit. Could revisit if the projection model
	// gains structure (Class, Method, etc.).
	SymbolKindFunction SymbolKind = 12
)

// Location is a URI + range pair, the standard LSP shape.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// SymbolInformation is the legacy workspace/symbol return shape.
// LSP 3.17 added WorkspaceSymbol with deferred location resolution,
// but the legacy form is universally supported and our payloads are
// small enough that lazy resolution buys nothing.
type SymbolInformation struct {
	Name          string     `json:"name"`
	Kind          SymbolKind `json:"kind"`
	Location      Location   `json:"location"`
	ContainerName string     `json:"containerName,omitempty"`
}

// Position is a 0-indexed line+character pair per the LSP spec.
// Distinct from config.SourceRange's 1-indexed lines: this is the
// wire format, where character is in UTF-16 code units.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range is a half-open span of text in a document.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Command identifies an editor-side command that runs when the user
// activates a CodeLens. Arguments is opaque - the editor extension
// passes it through to its registered handler verbatim. Tooltip is an
// optional hover string (used by the informational status lenses to
// explain the target or a fetch failure).
type Command struct {
	Title     string `json:"title"`
	Command   string `json:"command"`
	Tooltip   string `json:"tooltip,omitempty"`
	Arguments []any  `json:"arguments,omitempty"`
}

// CodeLens is a clickable annotation rendered inline in the editor.
// Title is plain text - editor extensions decorate with native
// icons via the Data.Intent field per the LSP plan.
type CodeLens struct {
	Range   Range         `json:"range"`
	Command *Command      `json:"command,omitempty"`
	Data    *CodeLensData `json:"data,omitempty"`
}

// CodeLensData carries the semantic intent so client extensions
// can map to a native icon / treatment without parsing the title.
type CodeLensData struct {
	Intent string `json:"intent"`
}

// CodeLensParams is the request payload for textDocument/codeLens.
type CodeLensParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// diagnosticSeverity matches LSP spec values: 1=Error, 2=Warning,
// 3=Information. (gaffer does not emit Hint.)
type diagnosticSeverity int

const (
	diagnosticSeverityError diagnosticSeverity = 1
	// (Warning, Information reserved by spec but not emitted today;
	// add when a Warning-level rule lands. gaffer does not emit Hint.)
)

// lspDiagnostic is the wire-format diagnostic. Disambiguates from
// config.Diagnostic, the upstream loose-validation shape.
type lspDiagnostic struct {
	Range    Range              `json:"range"`
	Severity diagnosticSeverity `json:"severity,omitempty"`
	Code     string             `json:"code,omitempty"`
	Source   string             `json:"source,omitempty"`
	Message  string             `json:"message"`
}

// PublishDiagnosticsParams is the payload for the server-pushed
// textDocument/publishDiagnostics notification. Empty Diagnostics
// clears the URI (drops squiggles).
type PublishDiagnosticsParams struct {
	URI         string          `json:"uri"`
	Diagnostics []lspDiagnostic `json:"diagnostics"`
}

// TextDocumentItem is the payload for didOpen: full URI, language,
// version, content. The server keeps the text in its document
// store; languageId is informational for V1 (we dispatch by file
// extension).
type TextDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

// VersionedTextDocumentIdentifier identifies a document at a
// specific client-side version. Used in didChange.
type VersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
}

// TextDocumentIdentifier identifies a document without versioning.
// Used in didClose / didSave.
type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

// TextDocumentContentChangeEvent under full sync (TextDocumentSync=1)
// always carries the entire new content in Text. Range / RangeLength
// are absent under full sync; we don't model them.
type TextDocumentContentChangeEvent struct {
	Text string `json:"text"`
}

// DidOpenTextDocumentParams: client opened a buffer.
type DidOpenTextDocumentParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

// DidChangeTextDocumentParams: client edited a buffer. ContentChanges
// has one element under full sync.
type DidChangeTextDocumentParams struct {
	TextDocument   VersionedTextDocumentIdentifier  `json:"textDocument"`
	ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
}

// DidCloseTextDocumentParams: client closed the buffer. Server
// drops its in-memory state for the URI.
type DidCloseTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// DidSaveTextDocumentParams: client saved. Text is optional (only
// present if the server requested it via the saveOptions
// capability, which we don't). For full-sync semantics we have
// the latest content from didChange already, so this is mostly
// informational.
type DidSaveTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Text         string                 `json:"text,omitempty"`
}

// FileChangeType matches LSP spec values: 1=Created, 2=Changed,
// 3=Deleted. Reported in workspace/didChangeWatchedFiles events.
type FileChangeType int

const (
	FileChangeCreated FileChangeType = 1
	FileChangeChanged FileChangeType = 2
	FileChangeDeleted FileChangeType = 3
)

// FileEvent is a single create/change/delete report on a watched
// path. Editors batch these into a DidChangeWatchedFilesParams.
type FileEvent struct {
	URI  string         `json:"uri"`
	Type FileChangeType `json:"type"`
}

// DidChangeWatchedFilesParams is the payload for the client-pushed
// workspace/didChangeWatchedFiles notification.
type DidChangeWatchedFilesParams struct {
	Changes []FileEvent `json:"changes"`
}

// Registration is one entry in a client/registerCapability request.
// The server uses this to dynamically register a watcher pattern
// after `initialized` (we can't statically advertise watchers via
// ServerCapabilities - LSP routes file watching through the editor,
// and the editor wants the pattern set at runtime).
type Registration struct {
	ID              string `json:"id"`
	Method          string `json:"method"`
	RegisterOptions any    `json:"registerOptions,omitempty"`
}

// RegistrationParams is the payload for the server->client
// client/registerCapability request.
type RegistrationParams struct {
	Registrations []Registration `json:"registrations"`
}

// ProjectionDetailsParams identifies a single projection by its
// declaring gaffer.toml URI plus its name (names are unique within
// a config but can repeat across configs in a multi-root workspace).
type ProjectionDetailsParams struct {
	ConfigURI string `json:"configURI"`
	Name      string `json:"name"`
}

// RefreshStatusParams identifies the gaffer.toml whose deploy status the
// editor wants re-fetched after an out-of-band auth change (e.g. sign-in).
type RefreshStatusParams struct {
	URI string `json:"uri"`
}

// ProjectionDetailsResult is the bits of a projection's parsed
// config that the editor needs to drive its run/debug source picker.
// Connection is the default env's connection string; nil means no
// default env (kept for back-compat). Fixtures is the projection's
// named-fixture list, alphabetically sorted. Environments is every
// configured [env.<name>] (name + whether it's the default), so the
// picker can offer non-default envs - matching the CodeLens picker.
type ProjectionDetailsResult struct {
	Connection   *string                 `json:"connection"`
	Fixtures     []string                `json:"fixtures"`
	Environments []config.EnvDescription `json:"environments,omitempty"`
}

// FileSystemWatcher is one entry in
// DidChangeWatchedFilesRegistrationOptions.Watchers. GlobPattern is
// a glob like `**/gaffer.toml`. Kind is a bitmask: 1=Create, 2=Change,
// 4=Delete; default (0/unset) is "all three" per spec.
type FileSystemWatcher struct {
	GlobPattern string `json:"globPattern"`
	Kind        int    `json:"kind,omitempty"`
}

// DidChangeWatchedFilesRegistrationOptions is the registerOptions
// payload for a workspace/didChangeWatchedFiles registration.
type DidChangeWatchedFilesRegistrationOptions struct {
	Watchers []FileSystemWatcher `json:"watchers"`
}
