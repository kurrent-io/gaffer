// gen-helpers emits cli/internal/telemetry/events.gen.go from the CUE schema
// at telemetry/schemas/.
//
// Design notes:
//
//   - The generator hardcodes the *structural* mapping (which CUE
//     definitions become which Go types, which command variants get a Tx
//     wrapper) but reads *values* from CUE: enum members, field lists per
//     event variant, optionality, and type kinds (string / bool / count /
//     enum / array / nested struct).
//
//   - Adding a new property to an existing command is a CUE-only edit: the
//     generator picks it up. Adding a brand-new command also requires a
//     one-line entry in the commands table below so the generator knows
//     whether to emit a Tx wrapper.
//
//   - The output is sorted/ordered deterministically so the CI determinism
//     check (regenerate + diff) reliably passes.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/load"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("gen-helpers: ")

	var (
		schemaDir string
		outPath   string
	)
	flag.StringVar(&schemaDir, "schemas", "telemetry/schemas", "directory containing events.cue + wire.cue")
	flag.StringVar(&outPath, "out", "cli/internal/telemetry/events.gen.go", "output Go file")
	flag.Parse()

	absSchemaDir, err := filepath.Abs(schemaDir)
	if err != nil {
		log.Fatalf("resolve schema dir: %v", err)
	}

	root, err := loadCUE(absSchemaDir)
	if err != nil {
		log.Fatalf("load cue: %v", err)
	}

	var buf bytes.Buffer
	if err := emitFile(&buf, root); err != nil {
		log.Fatalf("emit: %v", err)
	}

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		_ = os.WriteFile(outPath+".unformatted", buf.Bytes(), 0o644)
		log.Fatalf("gofmt failed (raw output at %s.unformatted): %v", outPath, err)
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		log.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(outPath, formatted, 0o644); err != nil {
		log.Fatalf("write: %v", err)
	}
}

func loadCUE(dir string) (cue.Value, error) {
	ctx := cuecontext.New()
	insts := load.Instances([]string{"."}, &load.Config{Dir: dir})
	if len(insts) != 1 {
		names := make([]string, 0, len(insts))
		for _, i := range insts {
			names = append(names, i.ID())
		}
		return cue.Value{}, fmt.Errorf("expected 1 cue instance in %s, got %d: %v", dir, len(insts), names)
	}
	if err := insts[0].Err; err != nil {
		return cue.Value{}, err
	}
	v := ctx.BuildInstance(insts[0])
	if err := v.Err(); err != nil {
		return cue.Value{}, err
	}
	// Validate structural soundness up front. cue.Concrete(false) skips the
	// "every leaf must be concrete" check we don't want for a schema, while
	// still flagging contradictions, missing references, etc.
	if err := v.Validate(cue.Concrete(false)); err != nil {
		return cue.Value{}, fmt.Errorf("schema validation: %w", err)
	}
	return v, nil
}

// ---------------------------------------------------------------------------
// Schema introspection (CUE -> Go-friendly shape)
// ---------------------------------------------------------------------------

// fieldKind discriminates how a CUE field maps to Go. Picked so the emitter
// can pick types and setter signatures without re-inspecting CUE.
type fieldKind int

const (
	kindString fieldKind = iota
	kindBool
	kindInt
	kindRawCount
	kindUUID
	kindTimestamp
	kindEnum         // see Ref
	kindIntEnum      // see Ref
	kindArrayOfEnum  // see Ref
	kindArrayOfString
	kindArrayOfStruct // see Ref
	kindStructRef    // see Ref
)

type schemaField struct {
	Wire     string
	GoName   string
	Optional bool
	Kind     fieldKind
	Ref      string // for kindEnum / kindArrayOfEnum / kindStructRef
	Doc      string
}

// commandSpec captures one entry in the command_invoked discriminated union.
type commandSpec struct {
	GoName  string // e.g. "Dev"
	CmdName string // wire literal, e.g. "dev"
	DefName string // CUE def, e.g. "DevCommandInvokedProperties"
	LongRun bool   // emit a Tx wrapper
}

// checkStringEnumsCoverage fails fast if the schema declared a top-level
// string-disjunction definition we haven't listed in stringEnums (or vice
// versa). Keeps the Go-side list honest about what's API surface.
func checkStringEnumsCoverage(root cue.Value, declared []string) error {
	declaredSet := map[string]bool{}
	for _, n := range declared {
		declaredSet[n] = true
	}
	iter, err := root.Fields(cue.Definitions(true))
	if err != nil {
		return err
	}
	discovered := map[string]bool{}
	for iter.Next() {
		sel := iter.Selector()
		if !sel.IsDefinition() {
			continue
		}
		name := strings.TrimPrefix(sel.String(), "#")
		v := iter.Value()
		// Only care about pure string disjunctions of >= 2 literals.
		if v.IncompleteKind() != cue.StringKind {
			continue
		}
		op, args := v.Expr()
		if op != cue.OrOp || len(args) < 2 {
			continue
		}
		if _, err := flattenStringDisjunction(v); err != nil {
			continue
		}
		discovered[name] = true
	}
	var missing, extra []string
	for n := range discovered {
		if !declaredSet[n] {
			missing = append(missing, n)
		}
	}
	for n := range declaredSet {
		if !discovered[n] {
			extra = append(extra, n)
		}
	}
	if len(missing) == 0 && len(extra) == 0 {
		return nil
	}
	sort.Strings(missing)
	sort.Strings(extra)
	return fmt.Errorf("stringEnums out of sync with schema: missing %v, extra %v", missing, extra)
}

// checkCommandsMatchEnum fails fast if the Go-side commands table has drifted
// from CUE's #CommandName disjunction. Catches the common error of adding a
// command in one place but not the other.
func checkCommandsMatchEnum(cmds []commandSpec, enumMembers []string) error {
	enumSet := map[string]bool{}
	for _, m := range enumMembers {
		enumSet[m] = true
	}
	tableSet := map[string]bool{}
	for _, c := range cmds {
		tableSet[c.CmdName] = true
	}
	var missing, extra []string
	for m := range enumSet {
		if !tableSet[m] {
			missing = append(missing, m)
		}
	}
	for m := range tableSet {
		if !enumSet[m] {
			extra = append(extra, m)
		}
	}
	if len(missing) == 0 && len(extra) == 0 {
		return nil
	}
	sort.Strings(missing)
	sort.Strings(extra)
	return fmt.Errorf("commands table out of sync with #CommandName: missing entries for %v, extra entries for %v", missing, extra)
}

// commands is the hardcoded list of command_invoked variants. Adding a new
// command requires adding the CUE definition and one entry here. The
// generator cross-checks this against #CommandName so drift fails loudly.
var commands = []commandSpec{
	{GoName: "Version", CmdName: "version", DefName: "VersionCommandInvokedProperties"},
	{GoName: "Init", CmdName: "init", DefName: "InitCommandInvokedProperties"},
	{GoName: "Scaffold", CmdName: "scaffold", DefName: "ScaffoldCommandInvokedProperties"},
	{GoName: "Info", CmdName: "info", DefName: "InfoCommandInvokedProperties"},
	{GoName: "Manifest", CmdName: "manifest", DefName: "ManifestCommandInvokedProperties"},
	{GoName: "Dev", CmdName: "dev", DefName: "DevCommandInvokedProperties", LongRun: true},
	{GoName: "MCP", CmdName: "mcp", DefName: "McpCommandInvokedProperties", LongRun: true},
	{GoName: "LSP", CmdName: "lsp", DefName: "LspCommandInvokedProperties", LongRun: true},
	{GoName: "Debug", CmdName: "debug", DefName: "DebugCommandInvokedProperties", LongRun: true},
}

// stringEnums lists the CUE definitions that the generator promotes to Go
// named string types. Each definition must be a disjunction of string
// literals. Order is the emit order.
var stringEnums = []string{
	"Emitter", "OS", "Arch", "RuntimeEnvironment",
	"CommandName",
	"Outcome", "ProjectionOutcome",
	"InvokedBy", "InvokedVia",
	"Editor", "CLIUnreachableReason",
	"ExceptionPhase",
}

// intEnums lists the CUE definitions that the generator promotes to Go named
// int types (one numeric literal per enum member).
var intEnums = []string{
	"FileSizeBucket",
}

func readStringEnum(root cue.Value, defName string) ([]string, error) {
	v := lookupDef(root, defName)
	if err := v.Err(); err != nil {
		return nil, fmt.Errorf("lookup #%s: %w", defName, err)
	}
	members, err := flattenStringDisjunction(v)
	if err != nil {
		return nil, fmt.Errorf("#%s: %w", defName, err)
	}
	uniq := map[string]struct{}{}
	out := make([]string, 0, len(members))
	for _, m := range members {
		if _, ok := uniq[m]; ok {
			continue
		}
		uniq[m] = struct{}{}
		out = append(out, m)
	}
	sort.Strings(out)
	return out, nil
}

func readIntEnum(root cue.Value, defName string) ([]int64, error) {
	v := lookupDef(root, defName)
	if err := v.Err(); err != nil {
		return nil, fmt.Errorf("lookup #%s: %w", defName, err)
	}
	members, err := flattenIntDisjunction(v)
	if err != nil {
		return nil, fmt.Errorf("#%s: %w", defName, err)
	}
	sort.Slice(members, func(i, j int) bool { return members[i] < members[j] })
	uniq := []int64{}
	for _, m := range members {
		if len(uniq) > 0 && uniq[len(uniq)-1] == m {
			continue
		}
		uniq = append(uniq, m)
	}
	return uniq, nil
}

// lookupDef returns the value at a top-level definition like `#Foo`. The
// cue.Def constructor accepts the name with or without the leading `#`; we
// strip it for clarity.
func lookupDef(root cue.Value, name string) cue.Value {
	return root.LookupPath(cue.MakePath(cue.Def(strings.TrimPrefix(name, "#"))))
}

// flattenStringDisjunction walks a value and returns all string literals
// reachable through OrOp branches, transitively (so #Outcome which OR's in
// #ProjectionOutcome flattens into a single member list).
func flattenStringDisjunction(v cue.Value) ([]string, error) {
	op, args := v.Expr()
	if op == cue.OrOp {
		var out []string
		for _, a := range args {
			sub, err := flattenStringDisjunction(a)
			if err != nil {
				return nil, err
			}
			out = append(out, sub...)
		}
		return out, nil
	}
	// Could be a reference to another enum def (e.g. ProjectionOutcome inside
	// Outcome). Expand by re-evaluating its expression.
	if _, ref := v.ReferencePath(); len(ref.Selectors()) > 0 {
		resolved := v.Eval()
		ropt, _ := resolved.Expr()
		if ropt == cue.OrOp {
			return flattenStringDisjunction(resolved)
		}
	}
	if v.Kind() != cue.StringKind {
		return nil, fmt.Errorf("non-string literal in disjunction: kind=%v at %s", v.Kind(), v.Path())
	}
	s, err := v.String()
	if err != nil {
		return nil, err
	}
	return []string{s}, nil
}

func flattenIntDisjunction(v cue.Value) ([]int64, error) {
	op, args := v.Expr()
	if op == cue.OrOp {
		var out []int64
		for _, a := range args {
			sub, err := flattenIntDisjunction(a)
			if err != nil {
				return nil, err
			}
			out = append(out, sub...)
		}
		return out, nil
	}
	if _, ref := v.ReferencePath(); len(ref.Selectors()) > 0 {
		resolved := v.Eval()
		ropt, _ := resolved.Expr()
		if ropt == cue.OrOp {
			return flattenIntDisjunction(resolved)
		}
	}
	if v.Kind() != cue.IntKind {
		return nil, fmt.Errorf("non-int literal in disjunction: kind=%v at %s", v.Kind(), v.Path())
	}
	i, err := v.Int64()
	if err != nil {
		return nil, err
	}
	return []int64{i}, nil
}

// readStructFields walks the fields of a CUE struct definition.
//
// fixedFields supplies a Go-name + kind override for fields that are inline
// nested structs in CUE. The generator emits these as synthesised named Go
// types (e.g. ProjectionShapeHandlers); CUE can't tell us those Go names.
func readStructFields(v cue.Value, fixedFields map[string]schemaField) ([]schemaField, error) {
	if k := v.IncompleteKind(); k&cue.StructKind == 0 {
		return nil, fmt.Errorf("not a struct (kind=%v)", k)
	}
	iter, err := v.Fields(cue.Optional(true), cue.Definitions(false))
	if err != nil {
		return nil, err
	}
	var out []schemaField
	for iter.Next() {
		sel := iter.Selector()
		if !sel.IsString() {
			continue
		}
		name := sel.Unquoted()
		fv := iter.Value()
		if fv.IncompleteKind() == cue.BottomKind {
			return nil, fmt.Errorf("field %s resolved to bottom: %v", name, fv.Err())
		}
		if override, ok := fixedFields[name]; ok {
			f := override
			f.Wire = name
			f.Optional = iter.IsOptional()
			f.Doc = firstDoc(fv)
			out = append(out, f)
			continue
		}
		f, err := classify(name, iter.IsOptional(), fv)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", name, err)
		}
		out = append(out, f)
	}
	return out, nil
}

func classify(name string, optional bool, v cue.Value) (schemaField, error) {
	doc := firstDoc(v)
	goName := snakeToCamel(name)

	// Resolve through a single-ref level so we can spot #BucketCount /
	// #UUID / #Timestamp / #FooEnum references.
	if ref, ok := singleDefRef(v); ok {
		switch ref {
		case "BucketCount":
			return schemaField{Wire: name, GoName: goName, Optional: optional, Kind: kindRawCount, Doc: doc}, nil
		case "UUID":
			return schemaField{Wire: name, GoName: goName, Optional: optional, Kind: kindUUID, Doc: doc}, nil
		case "Timestamp":
			return schemaField{Wire: name, GoName: goName, Optional: optional, Kind: kindTimestamp, Doc: doc}, nil
		}
		uk := v.IncompleteKind()
		if uk&cue.StringKind != 0 {
			return schemaField{Wire: name, GoName: goName, Optional: optional, Kind: kindEnum, Ref: ref, Doc: doc}, nil
		}
		if uk&cue.IntKind != 0 {
			return schemaField{Wire: name, GoName: goName, Optional: optional, Kind: kindIntEnum, Ref: ref, Doc: doc}, nil
		}
		if uk&cue.StructKind != 0 {
			return schemaField{Wire: name, GoName: goName, Optional: optional, Kind: kindStructRef, Ref: ref, Doc: doc}, nil
		}
		return schemaField{}, fmt.Errorf("unsupported ref %s (kind %v)", ref, uk)
	}

	switch {
	case isBoolField(v):
		return schemaField{Wire: name, GoName: goName, Optional: optional, Kind: kindBool, Doc: doc}, nil
	case isStringField(v):
		return schemaField{Wire: name, GoName: goName, Optional: optional, Kind: kindString, Doc: doc}, nil
	case v.IncompleteKind() == cue.IntKind:
		return schemaField{Wire: name, GoName: goName, Optional: optional, Kind: kindInt, Doc: doc}, nil
	case v.IncompleteKind()&cue.ListKind != 0:
		elem, ok := v.Elem()
		if !ok {
			return schemaField{}, fmt.Errorf("list elem missing")
		}
		if ref, ok := singleDefRef(elem); ok {
			if elem.IncompleteKind()&cue.StringKind != 0 {
				return schemaField{Wire: name, GoName: goName, Optional: optional, Kind: kindArrayOfEnum, Ref: ref, Doc: doc}, nil
			}
			if elem.IncompleteKind()&cue.StructKind != 0 {
				return schemaField{Wire: name, GoName: goName, Optional: optional, Kind: kindArrayOfStruct, Ref: ref, Doc: doc}, nil
			}
		}
		if elem.IncompleteKind()&cue.StringKind != 0 {
			return schemaField{Wire: name, GoName: goName, Optional: optional, Kind: kindArrayOfString, Doc: doc}, nil
		}
		return schemaField{}, fmt.Errorf("unsupported list element kind: %v", elem.IncompleteKind())
	}
	return schemaField{}, fmt.Errorf("unclassified field kind: %v", v.IncompleteKind())
}

// singleDefRef returns the trailing definition name in v's reference path, if
// any. e.g. for `field: #UUID` returns ("UUID", true).
func singleDefRef(v cue.Value) (string, bool) {
	_, path := v.ReferencePath()
	sels := path.Selectors()
	for i := len(sels) - 1; i >= 0; i-- {
		if sels[i].IsDefinition() {
			return strings.TrimPrefix(sels[i].String(), "#"), true
		}
	}
	return "", false
}

func isBoolField(v cue.Value) bool {
	return v.IncompleteKind() == cue.BoolKind
}

func isStringField(v cue.Value) bool {
	return v.IncompleteKind()&cue.StringKind != 0
}

func firstDoc(v cue.Value) string {
	docs := v.Doc()
	if len(docs) == 0 {
		return ""
	}
	var lines []string
	for _, d := range docs {
		for _, l := range strings.Split(d.Text(), "\n") {
			l = strings.TrimSpace(l)
			if l == "" {
				continue
			}
			lines = append(lines, l)
		}
	}
	return strings.Join(lines, " ")
}

// ---------------------------------------------------------------------------
// Emission
// ---------------------------------------------------------------------------

func emitFile(w io.Writer, root cue.Value) error {
	// Cross-check: every member of #CommandName must have a matching
	// commands-table entry, and every commands entry must match a
	// #CommandName member. Catches accidental drift between the two.
	cmdNames, err := readStringEnum(root, "CommandName")
	if err != nil {
		return fmt.Errorf("#CommandName: %w", err)
	}
	if err := checkCommandsMatchEnum(commands, cmdNames); err != nil {
		return err
	}
	if err := checkStringEnumsCoverage(root, stringEnums); err != nil {
		return err
	}

	mustWrite(w, fileHeader)

	for _, name := range stringEnums {
		members, err := readStringEnum(root, name)
		if err != nil {
			return err
		}
		emitStringEnum(w, name, members)
	}
	for _, name := range intEnums {
		members, err := readIntEnum(root, name)
		if err != nil {
			return err
		}
		emitIntEnum(w, name, members)
	}

	envelopeFields, err := readStructFields(
		lookupDef(root, "Envelope"),
		map[string]schemaField{
			"context": {GoName: "Context", Kind: kindStructRef, Ref: "Context"},
			// Events is a heterogeneous slice; each entry is one of the
			// generated event types (CommandInvoked / ProjectionShape /
			// ExtensionActivated / Exception). The Event marker interface
			// closes the set to package-internal implementations.
			"events": {GoName: "Events", Kind: kindStructRef, Ref: "[]Event"},
		},
	)
	if err != nil {
		return err
	}
	contextFields, err := readStructFields(lookupDef(root, "Context"), nil)
	if err != nil {
		return err
	}
	emitStruct(w, "Envelope", "Envelope is the top-level shape POSTed to the telemetry worker. One\nenvelope per HTTP request, carrying a batch of events that share the same\nemitter and run.", envelopeFields)
	emitStruct(w, "Context", "Context is per-envelope metadata describing the emitting environment.\nIdentical for every event in a batch.", contextFields)


	// command_invoked: base + per-variant structs + Tx wrappers.
	baseFields, err := readStructFields(lookupDef(root, "CommandInvokedBaseProperties"), nil)
	if err != nil {
		return err
	}
	emitCommandInvoked(w, baseFields)
	for _, cmd := range commands {
		all, err := readStructFields(lookupDef(root, cmd.DefName), nil)
		if err != nil {
			return fmt.Errorf("read %s: %w", cmd.DefName, err)
		}
		variant := filterBase(all, baseFields)
		emitCommandVariant(w, cmd, baseFields, variant)
		if cmd.LongRun {
			emitCommandTx(w, cmd, variant)
		}
	}

	// projection_shape.
	psFields, err := readStructFields(
		lookupDef(root, "ProjectionShapeProperties"),
		map[string]schemaField{
			"handlers":       {GoName: "Handlers", Kind: kindStructRef, Ref: "ProjectionShapeHandlers"},
			"builtin_counts": {GoName: "BuiltinCounts", Kind: kindStructRef, Ref: "ProjectionShapeBuiltinCounts"},
		},
	)
	if err != nil {
		return err
	}
	handlersDef := lookupDef(root, "ProjectionShapeProperties").LookupPath(cue.MakePath(cue.Str("handlers")))
	handlersFields, err := readStructFields(handlersDef, nil)
	if err != nil {
		return fmt.Errorf("handlers: %w", err)
	}
	builtinsDef := lookupDef(root, "ProjectionShapeProperties").LookupPath(cue.MakePath(cue.Str("builtin_counts")))
	builtinFields, err := readStructFields(builtinsDef, nil)
	if err != nil {
		return fmt.Errorf("builtin_counts: %w", err)
	}
	emitEventStruct(w, "ProjectionShape", "projection_shape", "ProjectionShape carries a snapshot of what a projection's source looks\nlike, structurally. Emitted on first encounter and again whenever the\nbucketed shape drifts.", psFields)
	emitStruct(w, "ProjectionShapeHandlers", "ProjectionShapeHandlers is the per-handler-kind set the projection registers.", handlersFields)
	emitStruct(w, "ProjectionShapeBuiltinCounts", "ProjectionShapeBuiltinCounts holds bucketed call counts for the allowlisted\nprojection builtins. Sparse: a builtin not called is absent in JSON.", builtinFields)

	// extension_activated.
	eaFields, err := readStructFields(lookupDef(root, "ExtensionActivatedProperties"), nil)
	if err != nil {
		return err
	}
	emitEventStruct(w, "ExtensionActivated", "extension_activated", "ExtensionActivated fires once on extension activation. The one event that\ncan fire when gaffer is otherwise unable to run - used to detect broken\ninstalls.", eaFields)

	// exception.
	exFields, err := readStructFields(lookupDef(root, "ExceptionProperties"), nil)
	if err != nil {
		return err
	}
	exEntryFields, err := readStructFields(
		lookupDef(root, "ExceptionEntry"),
		map[string]schemaField{
			"stacktrace": {GoName: "Stacktrace", Kind: kindStructRef, Ref: "ExceptionStacktrace"},
		},
	)
	if err != nil {
		return err
	}
	stackDef := lookupDef(root, "ExceptionEntry").LookupPath(cue.MakePath(cue.Str("stacktrace")))
	stackFields, err := readStructFields(stackDef, nil)
	if err != nil {
		return fmt.Errorf("stacktrace: %w", err)
	}
	frameFields, err := readStructFields(lookupDef(root, "Frame"), nil)
	if err != nil {
		return err
	}
	emitEventStruct(w, "Exception", "exception", "Exception captures gaffer-side crashes only - Go panics in the CLI / MCP,\nunhandled JS errors in the extension host, runtime exceptions in our own\n.NET code. Projection runtime errors surface as `outcome` on the relevant\n`command_invoked` event instead.", exFields)
	emitStruct(w, "ExceptionEntry", "ExceptionEntry is one exception in the causal chain.", exEntryFields)
	emitStruct(w, "ExceptionStacktrace", "ExceptionStacktrace is the structured stacktrace shape on every entry.", stackFields)
	emitStruct(w, "Frame", "Frame is one stack frame after structural scrubbing.", frameFields)

	return nil
}

// ---------------------------------------------------------------------------
// Emit helpers
// ---------------------------------------------------------------------------

func mustWrite(w io.Writer, s string) {
	if _, err := io.WriteString(w, s); err != nil {
		log.Fatalf("write: %v", err)
	}
}

const fileHeader = `// Code generated by telemetry/scripts/gen-helpers. DO NOT EDIT.
//
// This file holds the wire types (Envelope, Context, per-event Properties,
// enums, RawCount) and the per-command Tx wrappers with typed setters. The
// public emit / Begin / End helpers and the runtime that backs them
// (Client, Sink, panic-recover, ctx wiring) ship in hand-written companion
// files in this package - see telemetry.go and base.go. Until those land,
// the Tx types are not directly constructible; this file is intentionally
// runtime-free so the schema layer can compile in isolation.

package telemetry

import "encoding/json"

// UUID is a lowercase-hyphen UUID, no curly braces.
type UUID = string

// Timestamp is an RFC 3339 timestamp with millisecond precision.
type Timestamp = string

// SchemaVersion is the wire-schema version stamped on every envelope. Only
// bumped for breaking wire changes; additive changes keep the same version.
const SchemaVersion = "1"

// Event is the marker interface for every event type the generator emits.
// Implementations are closed by the unexported isEvent() method, so the
// Envelope.Events slice can hold any mix of generated event types but
// nothing from outside this package. Custom JSON unmarshal lives in a
// hand-written companion when an unmarshal path is needed.
type Event interface {
	isEvent()
}

// RawCount holds an unbucketed integer that bucket-rounds at marshal time to
// one of {0, 1, 2, 10, 100, 1000}. Producers store the raw count; the bucket
// math is applied at the JSON boundary so call sites never have to know the
// scheme.
type RawCount int

// MarshalJSON applies the bucket lookup: 0 / 1 / 2 / 10 / 100 / 1000 for the
// half-open intervals (-inf, 1) / [1, 2) / [2, 10) / [10, 100) / [100, 1000)
// / [1000, +inf). Negative values clamp to 0.
func (r RawCount) MarshalJSON() ([]byte, error) {
	switch n := int(r); {
	case n < 1:
		return []byte("0"), nil
	case n < 2:
		return []byte("1"), nil
	case n < 10:
		return []byte("2"), nil
	case n < 100:
		return []byte("10"), nil
	case n < 1000:
		return []byte("100"), nil
	default:
		return []byte("1000"), nil
	}
}

var _ json.Marshaler = RawCount(0)

`

func emitStringEnum(w io.Writer, name string, members []string) {
	fmt.Fprintf(w, "// %s is a string enum mirroring the CUE definition.\n", name)
	fmt.Fprintf(w, "type %s string\n\nconst (\n", name)
	for _, m := range members {
		fmt.Fprintf(w, "\t%s %s = %q\n", enumConst(name, m), name, m)
	}
	fmt.Fprint(w, ")\n\n")
}

func emitIntEnum(w io.Writer, name string, members []int64) {
	fmt.Fprintf(w, "// %s is an int enum mirroring the CUE definition.\n", name)
	fmt.Fprintf(w, "type %s int\n\nconst (\n", name)
	for _, m := range members {
		fmt.Fprintf(w, "\t%s %s = %d\n", fileSizeBucketConstName(name, m), name, m)
	}
	fmt.Fprint(w, ")\n\n")
}

func enumConst(typeName, member string) string {
	return typeName + snakeToCamel(member)
}

// fileSizeBucketConstName names int-enum members. Currently size-bucket-specific; we
// fail-fast if a different int enum gets added so the namer doesn't silently
// produce wrong identifiers for unrelated values.
func fileSizeBucketConstName(typeName string, member int64) string {
	if typeName != "FileSizeBucket" {
		log.Fatalf("fileSizeBucketConstName: unsupported int enum %q (member %d). Extend this function with a member-name scheme before adding new int enums.", typeName, member)
	}
	switch {
	case member < 1024:
		return typeName + "Under1KB"
	case member < 5*1024:
		return typeName + "1To5KB"
	case member < 20*1024:
		return typeName + "5To20KB"
	case member < 100*1024:
		return typeName + "20To100KB"
	default:
		return typeName + "Over100KB"
	}
}

func emitStruct(w io.Writer, name, doc string, fields []schemaField) {
	emitDoc(w, doc)
	fmt.Fprintf(w, "type %s struct {\n", name)
	emitFields(w, fields)
	fmt.Fprint(w, "}\n\n")
}

func emitEventStruct(w io.Writer, goName, wireName, doc string, propFields []schemaField) {
	_ = wireName
	emitDoc(w, doc)
	fmt.Fprintf(w, "type %s struct {\n", goName)
	fmt.Fprintf(w, "\tName       string       `json:\"name\"`\n")
	fmt.Fprintf(w, "\tTimestamp  Timestamp    `json:\"timestamp\"`\n")
	fmt.Fprintf(w, "\tProperties %sProperties `json:\"properties\"`\n", goName)
	fmt.Fprint(w, "}\n\n")
	fmt.Fprintf(w, "func (%s) isEvent() {}\n\n", goName)

	emitDoc(w, fmt.Sprintf("%sProperties holds the property fields for the %s event.", goName, wireName))
	fmt.Fprintf(w, "type %sProperties struct {\n", goName)
	emitFields(w, propFields)
	fmt.Fprint(w, "}\n\n")
}

func emitCommandInvoked(w io.Writer, base []schemaField) {
	emitDoc(w, "CommandInvoked fires once at the end of every CLI invocation - one-shot or\nlong-running. The wire shape carries a discriminated union of variant\nproperty structs in `properties`; Go can't express that directly, so the\nfield is `any` and producers assign the per-variant struct\n(VersionCommandInvokedProperties, DevCommandInvokedProperties, ...) they\nbuilt up via the matching Tx.")
	fmt.Fprint(w, "type CommandInvoked struct {\n")
	fmt.Fprintf(w, "\tName       string    `json:\"name\"`\n")
	fmt.Fprintf(w, "\tTimestamp  Timestamp `json:\"timestamp\"`\n")
	fmt.Fprintf(w, "\tProperties any       `json:\"properties\"`\n")
	fmt.Fprint(w, "}\n\n")
	fmt.Fprint(w, "func (CommandInvoked) isEvent() {}\n\n")

	emitDoc(w, "CommandInvokedProperties holds the base fields present on every variant\nof command_invoked. Kept as a standalone type for documentation /\nshape reference; per-command variants inline these fields directly\nrather than embedding so call sites stay flat (no nested struct\nliteral required to set Outcome).")
	fmt.Fprint(w, "type CommandInvokedProperties struct {\n")
	emitFields(w, base)
	fmt.Fprint(w, "}\n\n")
}

func emitCommandVariant(w io.Writer, cmd commandSpec, base, variant []schemaField) {
	doc := fmt.Sprintf("%sCommandInvokedProperties carries the property set for `gaffer %s`.\nBase fields are inlined rather than embedded so callers can set\nOutcome (and future per-invocation overrides) at the top-level\nstruct literal without an extra nesting layer.", cmd.GoName, cmd.CmdName)
	emitDoc(w, doc)
	fmt.Fprintf(w, "type %sCommandInvokedProperties struct {\n", cmd.GoName)
	emitFields(w, base)
	emitFields(w, variant)
	fmt.Fprint(w, "}\n\n")
}

func emitCommandTx(w io.Writer, cmd commandSpec, variant []schemaField) {
	doc := fmt.Sprintf("%sTx accumulates `%s` command properties over the lifetime of the\ninvocation. Single-goroutine-owned: setters are NOT safe for concurrent use -\ndrain counters on the main goroutine before End() time.", cmd.GoName, cmd.CmdName)
	emitDoc(w, doc)
	fmt.Fprintf(w, "type %sTx struct {\n\tprops %sCommandInvokedProperties\n}\n\n", cmd.GoName, cmd.GoName)

	// Properties accessor for the runtime to use at End() time.
	fmt.Fprintf(w, "// Properties returns a snapshot of the accumulated properties for marshal.\n")
	fmt.Fprintf(w, "func (tx *%sTx) Properties() %sCommandInvokedProperties { return tx.props }\n\n", cmd.GoName, cmd.GoName)

	// Outcome setter on every Tx (uses the base field). Nil-safe
	// so the cobra RunE doesn't need a `if tx != nil` guard at each
	// call site - matches the rest of the package's nil-tolerance
	// pattern (Flush, ClientFromContext, End).
	fmt.Fprintf(w, "// SetOutcome records the final outcome for the invocation.\n// Nil-safe: silent no-op on a nil receiver.\n")
	fmt.Fprintf(w, "func (tx *%sTx) SetOutcome(o Outcome) {\n\tif tx == nil {\n\t\treturn\n\t}\n\ttx.props.Outcome = o\n}\n\n", cmd.GoName)

	for _, f := range variant {
		emitTxSetter(w, cmd.GoName, f)
	}
}

func emitTxSetter(w io.Writer, txName string, f schemaField) {
	// Setters only target optional fields - those are stored as pointers /
	// nil-able slices, so `&v` storage works. Non-optional variant fields
	// are guaranteed by the wire schema and don't need a setter.
	if !f.Optional {
		log.Fatalf("emitTxSetter: non-optional field %q on %s. Variant Tx fields should all be optional (the wire schema requires them); add a special case or change the schema.", f.Wire, txName)
	}
	doc := f.Doc
	if doc == "" {
		doc = fmt.Sprintf("Set%s records %s.", f.GoName, f.Wire)
	} else {
		doc = fmt.Sprintf("Set%s records %s. %s", f.GoName, f.Wire, doc)
	}
	// Doc gets a nil-safety hint appended; the body opens with a
	// nil-receiver guard so the cobra RunE can call setters without
	// branching on Begin's return (which is nil when telemetry is
	// off).
	emitDoc(w, doc+" Nil-safe: silent no-op on a nil receiver.")
	switch f.Kind {
	case kindRawCount:
		fmt.Fprintf(w, "func (tx *%sTx) Set%s(n int) {\n\tif tx == nil {\n\t\treturn\n\t}\n\tv := RawCount(n)\n\ttx.props.%s = &v\n}\n\n", txName, f.GoName, f.GoName)
	case kindBool:
		fmt.Fprintf(w, "func (tx *%sTx) Set%s(b bool) {\n\tif tx == nil {\n\t\treturn\n\t}\n\ttx.props.%s = &b\n}\n\n", txName, f.GoName, f.GoName)
	case kindString:
		fmt.Fprintf(w, "func (tx *%sTx) Set%s(s string) {\n\tif tx == nil {\n\t\treturn\n\t}\n\ttx.props.%s = &s\n}\n\n", txName, f.GoName, f.GoName)
	case kindArrayOfString:
		fmt.Fprintf(w, "func (tx *%sTx) Set%s(v []string) {\n\tif tx == nil {\n\t\treturn\n\t}\n\ttx.props.%s = v\n}\n\n", txName, f.GoName, f.GoName)
	case kindArrayOfEnum:
		fmt.Fprintf(w, "func (tx *%sTx) Set%s(v []%s) {\n\tif tx == nil {\n\t\treturn\n\t}\n\ttx.props.%s = v\n}\n\n", txName, f.GoName, f.Ref, f.GoName)
	case kindEnum:
		fmt.Fprintf(w, "func (tx *%sTx) Set%s(v %s) {\n\tif tx == nil {\n\t\treturn\n\t}\n\ttx.props.%s = &v\n}\n\n", txName, f.GoName, f.Ref, f.GoName)
	default:
		log.Fatalf("emitTxSetter: unsupported field kind for setter on %s.Set%s (kind=%d). Extend emitTxSetter or remove the field from the variant.", txName, f.GoName, f.Kind)
	}
}

func emitFields(w io.Writer, fields []schemaField) {
	for _, f := range fields {
		emitField(w, f)
	}
}

func emitField(w io.Writer, f schemaField) {
	emitDoc(w, f.Doc)
	goType := goTypeFor(f)
	tag := f.Wire
	if f.Optional {
		tag += ",omitempty"
	}
	fmt.Fprintf(w, "\t%s %s `json:%q`\n", f.GoName, goType, tag)
}

func goTypeFor(f schemaField) string {
	var base string
	switch f.Kind {
	case kindString:
		base = "string"
	case kindUUID:
		base = "UUID"
	case kindTimestamp:
		base = "Timestamp"
	case kindBool:
		base = "bool"
	case kindInt:
		base = "int"
	case kindRawCount:
		base = "RawCount"
	case kindEnum, kindIntEnum:
		base = f.Ref
	case kindStructRef:
		if strings.HasPrefix(f.Ref, "[]") {
			return f.Ref
		}
		base = f.Ref
	case kindArrayOfEnum:
		return "[]" + f.Ref
	case kindArrayOfStruct:
		return "[]" + f.Ref
	case kindArrayOfString:
		return "[]string"
	default:
		base = "any"
	}
	if f.Optional && !isArrayKind(f.Kind) && f.Kind != kindStructRef {
		return "*" + base
	}
	return base
}

func isArrayKind(k fieldKind) bool {
	return k == kindArrayOfString || k == kindArrayOfEnum || k == kindArrayOfStruct
}

func emitDoc(w io.Writer, doc string) {
	if doc == "" {
		return
	}
	for _, line := range strings.Split(doc, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fmt.Fprintf(w, "// %s\n", line)
	}
}

// filterBase removes fields whose wire names already appear in the base list.
func filterBase(all, base []schemaField) []schemaField {
	skip := map[string]bool{}
	for _, f := range base {
		skip[f.Wire] = true
	}
	var out []schemaField
	for _, f := range all {
		if skip[f.Wire] {
			continue
		}
		out = append(out, f)
	}
	return out
}

// initialisms maps lowercase tokens that should be capitalised wholesale
// when assembling a Go identifier from a snake_case wire name. Adding a new
// initialism is one line here plus a one-line test case in
// snake_to_camel_test.go.
var initialisms = map[string]string{
	"ci":       "CI",
	"cli":      "CLI",
	"dap":      "DAP",
	"db":       "DB",
	"id":       "ID",
	"ia32":     "IA32",
	"json":     "JSON",
	"lsp":      "LSP",
	"mcp":      "MCP",
	"ms":       "Ms",
	"os":       "OS",
	"uri":      "URI",
	"url":      "URL",
	"vscode":   "VSCode",
	"vscodium": "VSCodium",
}

// snakeToCamel: "manifest_features_used" -> "ManifestFeaturesUsed".
func snakeToCamel(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		if init, ok := initialisms[strings.ToLower(p)]; ok {
			parts[i] = init
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "")
}
