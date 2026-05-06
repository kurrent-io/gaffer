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
