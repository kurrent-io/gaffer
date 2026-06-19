using System.Text;
using Acornima;
using Acornima.Ast;
using Gaffer.Sdk;
using Gaffer.Sdk.Diagnostics;
using Gaffer.Sdk.Versioning;

namespace Gaffer.Runtime.Projection.Diagnostics;

/// <summary>
/// Walks a projection's AST at compile time, running each registered
/// <see cref="IRule"/> and collecting any <see cref="Diagnostic"/>s they emit.
/// New rules plug in by adding to <see cref="Rules"/>.
/// </summary>
internal static class DiagnosticCollector {
	// Add new rules here. Each owns its own AST walk.
	private static readonly IRule[] Rules = new IRule[] {
		new LinkStreamToDeprecationRule(),
		new LinkStreamToOutOfBoundsParametersRule(),
		new LogMultiParamRule(),
		new TransformsNotAppliedInV2Rule(),
		new OutputStateNoEffectOnV2Rule(),
		new DuplicateOptionsRule(),
		new ReorderEventsNoEffectOnV2Rule(),
		new AsyncHandlerRule(),
	};

	// Definition-based rules run off the resolved QuerySources rather than the AST, so they can be
	// authoritative about engine-version-specific limitations (e.g. bi-state on V2) that an AST
	// scan could only guess at.
	private static readonly IDefinitionRule[] DefinitionRules = new IDefinitionRule[] {
		new BiStateUnsupportedOnV2Rule(),
	};

	/// <summary>
	/// Parse <paramref name="source"/> and run every rule. Returns the
	/// collected diagnostics, or <c>null</c> if there are none.
	/// <para>
	/// Diagnostics are best-effort. We swallow parse failures (Acornima/Jint
	/// option drift) and per-rule exceptions so a diagnostic bug never breaks
	/// an otherwise-valid projection. The user just doesn't get diagnostics.
	/// </para>
	/// </summary>
	public static Diagnostic[]? Scan(string source, KurrentDbVersion? quirksVersion, ProjectionVersion engineVersion) {
		var (diagnostics, _, _) = ScanWithShape(source, quirksVersion, engineVersion, includeShape: false);
		return diagnostics;
	}

	/// <summary>
	/// Parse <paramref name="source"/> once, run diagnostic rules, and
	/// optionally run the <see cref="ShapeCollector"/> walker against
	/// the same AST. Returns both results so callers requesting shape
	/// data don't pay for a second parse.
	/// <para>
	/// Reachability of the <c>Parsable = false</c> sentinel: the
	/// <see cref="ProjectionSession"/> constructor parses the source
	/// via Jint *first*, throwing
	/// <see cref="Gaffer.Runtime.Errors.InvalidProjectionException"/>
	/// on syntax errors before <see cref="ScanWithShape"/> runs. So
	/// the dominant "user wrote bad JS" case never produces this
	/// sentinel; it surfaces as a thrown exception and the calling
	/// command's telemetry records the failure via
	/// <c>command_invoked.outcome</c> instead. The sentinel only
	/// fires on the rarer parser-drift case: Jint accepted the
	/// source but Acornima rejects it. We still surface it so
	/// the worker can distinguish "shape unavailable" from
	/// "shape skipped".
	/// </para>
	/// </summary>
	public static (Diagnostic[]? diagnostics, ProjectionShape? shape, bool emitsEvents) ScanWithShape(
		string source,
		KurrentDbVersion? quirksVersion,
		ProjectionVersion engineVersion,
		bool includeShape,
		QuerySources? definition = null) {
		Script ast;
		try {
			ast = new Parser().ParseScript(source, "projection.js");
		} catch {
			// Acornima rejected the source but Jint accepted it (parser drift). Definition rules
			// don't read the AST, so still run them off the resolved definition - an engine-version
			// limitation like bi-state on V2 must surface even when the AST scan can't. Emit-ness
			// can't be detected without the AST, so report false (same limitation as a null shape).
			var defOnly = new List<Diagnostic>();
			if (definition is not null)
				RunDefinitionRules(definition, quirksVersion, engineVersion, defOnly);
			return (defOnly.Count > 0 ? defOnly.ToArray() : null, includeShape ? UnparsableShape(source) : null, false);
		}
		var diagnostics = new List<Diagnostic>();
		foreach (var rule in Rules) {
			try {
				rule.Run(ast, quirksVersion, engineVersion, diagnostics);
			} catch {
				// One rule failing doesn't taint the others.
			}
		}
		if (definition is not null)
			RunDefinitionRules(definition, quirksVersion, engineVersion, diagnostics);
		// Emit detection rides this always-on parse (not gated by includeShape),
		// so emit-ness is known on every compile without the shape walk.
		bool emitsEvents = EmitDetector.Detect(ast);
		ProjectionShape? shape = includeShape
			? ShapeCollector.Walk(ast, FileSizeBytes(source), parsable: true)
			: null;
		return (diagnostics.Count > 0 ? diagnostics.ToArray() : null, shape, emitsEvents);
	}

	private static void RunDefinitionRules(QuerySources definition, KurrentDbVersion? quirksVersion, ProjectionVersion engineVersion, List<Diagnostic> diagnostics) {
		foreach (var rule in DefinitionRules) {
			try {
				rule.Run(definition, quirksVersion, engineVersion, diagnostics);
			} catch {
				// One rule failing doesn't taint the others.
			}
		}
	}

	// Sentinel "Acornima parse failed" shape: zero builtin counts,
	// no handlers, file size carried through. Distinguishable on
	// the wire by `parsable: false` - downstream consumers MUST
	// include Parsable in any dedupe / content-hash domain so this
	// doesn't collapse with a valid empty projection.
	private static ProjectionShape UnparsableShape(string source) => new() {
		Parsable = false,
		FileSize = FileSizeBytes(source),
	};

	// C# `string.Length` is UTF-16 code units, not bytes. For
	// non-ASCII projections that under-counts. The wire field is
	// `file_size` documented as bytes; honor the unit at this
	// boundary so downstream bucket math is honest.
	private static int FileSizeBytes(string source) =>
		Encoding.UTF8.GetByteCount(source);

	// Acornima: line 1-based, column 0-based. Sdk: both 1-based.
	// Acornima.SourceLocation fully qualified to avoid confusion with our
	// SourceRange/SourcePosition.
	internal static SourceRange ToSourceRange(Acornima.SourceLocation loc) => new() {
		Start = new SourcePosition { Line = loc.Start.Line, Column = loc.Start.Column + 1 },
		End = new SourcePosition { Line = loc.End.Line, Column = loc.End.Column + 1 },
	};
}

internal interface IRule {
	void Run(Script ast, KurrentDbVersion? quirksVersion, ProjectionVersion engineVersion, List<Diagnostic> diagnostics);
}

internal interface IDefinitionRule {
	void Run(QuerySources definition, KurrentDbVersion? quirksVersion, ProjectionVersion engineVersion, List<Diagnostic> diagnostics);
}
