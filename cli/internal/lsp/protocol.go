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
