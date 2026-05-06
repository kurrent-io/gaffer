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

import "encoding/json"

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
)

// Gaffer command IDs surfaced via CodeLens.command. Each editor
// extension routes these to its native debug-launch API.
const (
	CommandDebugProjection     = "gaffer.debugProjection"
	CommandDebugProjectionPick = "gaffer.debugProjectionPick"
)

// InitializeParams is the subset of LSP InitializeParams we care
// about. Real LSP InitializeParams has dozens of fields; unmodelled
// fields stay in the wire JSON without forcing us to commit to a
// shape. InitOptions is held as json.RawMessage so the eventual
// consumer can decode just-in-time, preserving forward compat and
// avoiding integer-as-float64 coercion that map[string]interface{}
// would force.
type InitializeParams struct {
	ProcessID        *int               `json:"processId,omitempty"`
	RootURI          string             `json:"rootUri,omitempty"`
	WorkspaceFolders []WorkspaceFolder  `json:"workspaceFolders,omitempty"`
	Capabilities     ClientCapabilities `json:"capabilities"`
	InitOptions      json.RawMessage    `json:"initializationOptions,omitempty"`
}

// WorkspaceFolder is an entry in InitializeParams.WorkspaceFolders.
type WorkspaceFolder struct {
	URI  string `json:"uri"`
	Name string `json:"name"`
}

// ClientCapabilities is intentionally empty for V1 - we don't gate
// behavior on what the client supports, only on what we emit. Add
// fields as we start using them.
type ClientCapabilities struct{}

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
	// TextDocumentSync = 1 means "full document sync" - the client
	// re-sends the entire document on each change. See LSP plan
	// Decision 1: we explicitly chose full sync over incremental
	// since config files are tiny.
	TextDocumentSync int `json:"textDocumentSync"`
	// CodeLensProvider advertises that the server responds to
	// textDocument/codeLens requests. Empty options struct is
	// fine - we don't require resolveProvider since lenses are
	// fully populated on the initial response.
	CodeLensProvider *CodeLensOptions `json:"codeLensProvider,omitempty"`
}

// CodeLensOptions is the value of ServerCapabilities.CodeLensProvider.
type CodeLensOptions struct {
	// ResolveProvider would be true if the server wanted clients
	// to call back via codeLens/resolve to fill in lens details
	// lazily. We populate everything upfront so it's false/absent.
	ResolveProvider bool `json:"resolveProvider,omitempty"`
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
// passes it through to its registered handler verbatim.
type Command struct {
	Title     string        `json:"title"`
	Command   string        `json:"command"`
	Arguments []interface{} `json:"arguments,omitempty"`
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

// DiagnosticSeverity matches LSP spec values: 1=Error, 2=Warning,
// 3=Information, 4=Hint.
type DiagnosticSeverity int

const (
	DiagnosticSeverityError       DiagnosticSeverity = 1
	DiagnosticSeverityWarning     DiagnosticSeverity = 2
	DiagnosticSeverityInformation DiagnosticSeverity = 3
	DiagnosticSeverityHint        DiagnosticSeverity = 4
)

// LSPDiagnostic is the wire-format diagnostic. Named to disambiguate
// from config.Diagnostic which is the upstream loose-validation
// shape.
type LSPDiagnostic struct {
	Range    Range              `json:"range"`
	Severity DiagnosticSeverity `json:"severity,omitempty"`
	Code     string             `json:"code,omitempty"`
	Source   string             `json:"source,omitempty"`
	Message  string             `json:"message"`
}

// PublishDiagnosticsParams is the payload for the server-pushed
// textDocument/publishDiagnostics notification. Empty Diagnostics
// clears the URI (drops squiggles).
type PublishDiagnosticsParams struct {
	URI         string          `json:"uri"`
	Diagnostics []LSPDiagnostic `json:"diagnostics"`
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
