using System.Text;
using Acornima;
using Acornima.Ast;
using Gaffer.Sdk;
using Gaffer.Sdk.Diagnostics;
using Gaffer.Sdk.Versioning;

namespace Gaffer.Runtime.Projection;

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
		new OutputStateUnconditionalInV2Rule(),
		new DuplicateOptionsRule(),
		new ReorderEventsNoEffectOnV2Rule(),
		new AsyncHandlerRule(),
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
		var (diagnostics, _) = ScanWithShape(source, quirksVersion, engineVersion, includeShape: false);
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
	public static (Diagnostic[]? diagnostics, ProjectionShape? shape) ScanWithShape(
		string source,
		KurrentDbVersion? quirksVersion,
		ProjectionVersion engineVersion,
		bool includeShape) {
		Script ast;
		try {
			ast = new Parser().ParseScript(source, "projection.js");
		} catch {
			return (null, includeShape ? UnparsableShape(source) : null);
		}
		var diagnostics = new List<Diagnostic>();
		foreach (var rule in Rules) {
			try {
				rule.Run(ast, quirksVersion, engineVersion, diagnostics);
			} catch {
				// One rule failing doesn't taint the others.
			}
		}
		ProjectionShape? shape = includeShape
			? ShapeCollector.Walk(ast, FileSizeBytes(source), parsable: true)
			: null;
		return (diagnostics.Count > 0 ? diagnostics.ToArray() : null, shape);
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

	internal interface IRule {
		void Run(Script ast, KurrentDbVersion? quirksVersion, ProjectionVersion engineVersion, List<Diagnostic> diagnostics);
	}

	// Acornima: line 1-based, column 0-based. Sdk: both 1-based.
	// Acornima.SourceLocation fully qualified to avoid confusion with our
	// SourceRange/SourcePosition.
	internal static SourceRange ToSourceRange(Acornima.SourceLocation loc) => new() {
		Start = new SourcePosition { Line = loc.Start.Line, Column = loc.Start.Column + 1 },
		End = new SourcePosition { Line = loc.End.Line, Column = loc.End.Column + 1 },
	};

	/// <summary>
	/// Scans for calls to a named global identifier, with shadow detection.
	/// A top-level <c>var</c>/<c>function</c> declaration of the same name
	/// flips <see cref="Shadowed"/>; rules that depend on the global then
	/// suppress their diagnostics. <paramref name="matchCall"/> filters which
	/// calls land in <see cref="Calls"/> (e.g. arity gate).
	/// </summary>
	private sealed class IdentifierShadowScanner : AstVisitor {
		private readonly string _name;
		private readonly Func<CallExpression, bool> _matchCall;

		public IdentifierShadowScanner(string name, Func<CallExpression, bool> matchCall) {
			_name = name;
			_matchCall = matchCall;
		}

		public bool Shadowed { get; private set; }
		public List<Acornima.SourceLocation> Calls { get; } = new();

		protected override object? VisitVariableDeclarator(VariableDeclarator node) {
			if (node.Id is Identifier id && id.Name == _name)
				Shadowed = true;
			return base.VisitVariableDeclarator(node);
		}

		protected override object? VisitFunctionDeclaration(FunctionDeclaration node) {
			if (node.Id is Identifier id && id.Name == _name)
				Shadowed = true;
			return base.VisitFunctionDeclaration(node);
		}

		protected override object? VisitCallExpression(CallExpression node) {
			if (node.Callee is Identifier callee && callee.Name == _name && _matchCall(node))
				Calls.Add(callee.Location);
			return base.VisitCallExpression(node);
		}
	}

	private sealed class LinkStreamToDeprecationRule : IRule {
		public void Run(Script ast, KurrentDbVersion? quirksVersion, ProjectionVersion engineVersion, List<Diagnostic> diagnostics) {
			// Deprecation is independent of quirksVersion - linkStreamTo is
			// undocumented at every released version we know about.
			var scanner = new IdentifierShadowScanner("linkStreamTo", _ => true);
			scanner.Visit(ast);
			if (scanner.Shadowed)
				return;

			foreach (var loc in scanner.Calls) {
				diagnostics.Add(DiagnosticCatalog.LinkStreamToDeprecated.ToDiagnostic(ToSourceRange(loc)));
			}
		}
	}

	private sealed class LinkStreamToOutOfBoundsParametersRule : IRule {
		public void Run(Script ast, KurrentDbVersion? quirksVersion, ProjectionVersion engineVersion, List<Diagnostic> diagnostics) {
			if (!DiagnosticCatalog.LinkStreamToOutOfBoundsParameters.FiresAt(quirksVersion))
				return;

			// 3+ args triggers the quirk. 2-arg form is fine.
			var scanner = new IdentifierShadowScanner("linkStreamTo", call => call.Arguments.Count >= 3);
			scanner.Visit(ast);
			// Shadowed local linkStreamTo masks the upstream quirk entirely -
			// the call goes to the user's function, not the quirky global.
			if (scanner.Shadowed)
				return;

			foreach (var loc in scanner.Calls) {
				diagnostics.Add(DiagnosticCatalog.LinkStreamToOutOfBoundsParameters.ToDiagnostic(ToSourceRange(loc)));
			}
		}
	}

	private sealed class LogMultiParamRule : IRule {
		public void Run(Script ast, KurrentDbVersion? quirksVersion, ProjectionVersion engineVersion, List<Diagnostic> diagnostics) {
			if (!DiagnosticCatalog.LogMultiParam.FiresAt(quirksVersion))
				return;

			// No shadow check: gaffer (and KurrentDB) registers `log` as a
			// non-configurable, non-writable global, so top-level
			// `var log = ...` / `function log() {}` collides at engine
			// initialisation and the projection won't even compile.
			// Inner-scope shadows are possible in theory but rare in practice.
			var scanner = new Scanner();
			scanner.Visit(ast);

			foreach (var loc in scanner.ProblematicCalls) {
				diagnostics.Add(DiagnosticCatalog.LogMultiParam.ToDiagnostic(ToSourceRange(loc)));
			}
		}

		private sealed class Scanner : AstVisitor {
			public readonly List<Acornima.SourceLocation> ProblematicCalls = new();

			protected override object? VisitCallExpression(CallExpression node) {
				// 2+ args triggers the upstream multi-param quirk. 1-arg path is fine.
				if (node.Callee is Identifier { Name: "log" } id && node.Arguments.Count >= 2)
					ProblematicCalls.Add(id.Location);
				return base.VisitCallExpression(node);
			}
		}
	}

	/// <summary>
	/// Scans for chained method calls of a named property (e.g. <c>x.foo()</c>).
	/// Used for transforms/outputState which are chain methods on the
	/// projection runtime, not globals - so shadow detection (which exists
	/// for global identifiers in <see cref="IdentifierShadowScanner"/>)
	/// doesn't apply: a property name on a chain object can't be shadowed
	/// by a top-level <c>var</c>/<c>function</c>.
	/// </summary>
	private sealed class MemberCallScanner : AstVisitor {
		private readonly string _name;

		public MemberCallScanner(string name) {
			_name = name;
		}

		public List<Acornima.SourceLocation> Calls { get; } = new();

		protected override object? VisitCallExpression(CallExpression node) {
			if (node.Callee is MemberExpression me &&
				!me.Computed &&
				me.Property is Identifier { Name: var propName } prop &&
				propName == _name) {
				Calls.Add(prop.Location);
			}
			return base.VisitCallExpression(node);
		}
	}

	// Predicate is `== V2` rather than `<= V2.x`: when V2 grows transforms
	// in some future engine version, the rule should stop firing for that
	// version, not start firing for *future* versions before they exist.
	// Re-evaluate this gate when a third engine version lands.
	private sealed class TransformsNotAppliedInV2Rule : IRule {
		public void Run(Script ast, KurrentDbVersion? quirksVersion, ProjectionVersion engineVersion, List<Diagnostic> diagnostics) {
			if (engineVersion != ProjectionVersion.V2)
				return;

			// transformBy / filterBy: in V2 the engine never iterates
			// _transforms, so any function passed here is registered but
			// never invoked on events. Surface as a Warning so the user
			// finds out before they wonder why their result stream is just
			// the state.
			ScanAndEmit("transformBy", ast, diagnostics);
			ScanAndEmit("filterBy", ast, diagnostics);
		}

		private static void ScanAndEmit(string name, Script ast, List<Diagnostic> diagnostics) {
			var scanner = new MemberCallScanner(name);
			scanner.Visit(ast);
			foreach (var loc in scanner.Calls) {
				diagnostics.Add(DiagnosticCatalog.TransformsNotInvoked.ToDiagnostic(ToSourceRange(loc)));
			}
		}
	}

	// options() is last-write-wins: a second call silently discards the
	// first. That's almost always a refactor mistake (a stale options
	// block left behind), so warn on every call past the first. Not
	// quirk- or version-gated - it's a usage lint, true at all versions.
	private sealed class DuplicateOptionsRule : IRule {
		public void Run(Script ast, KurrentDbVersion? quirksVersion, ProjectionVersion engineVersion, List<Diagnostic> diagnostics) {
			var scanner = new IdentifierShadowScanner("options", _ => true);
			scanner.Visit(ast);
			// A top-level `var options` / `function options` shadows the
			// definition global, so these calls aren't the real options().
			if (scanner.Shadowed || scanner.Calls.Count <= 1)
				return;

			// Skip the first call; flag each later one as the duplicate.
			foreach (var loc in scanner.Calls.Skip(1)) {
				diagnostics.Add(DiagnosticCatalog.OptionsDuplicate.ToDiagnostic(ToSourceRange(loc)));
			}
		}
	}

	// reorderEvents / processingLag are silently ignored under engine_version 2: V2's
	// ReadStrategyFactory never reads them, so events run in arrival order regardless of source.
	// Warn so the user finds out the option is dead rather than wondering why ordering didn't
	// change. The V1 path is the opposite - KurrentDB's ReaderStrategy rejects reorderEvents on
	// the wrong source/lag with a hard error - and is reproduced as a throw at session-create in
	// ProjectionSession, off the resolved QuerySources (authoritative; a throw must be exact).
	// This rule is the editor-facing V2 hint and reads the AST literally (best-effort, with a
	// source range), so it can't see a computed reorderEvents value - acceptable for a warning.
	//
	// processingLag alone (no reorderEvents) is a no-op on every engine, not a V2 regression, so
	// it isn't flagged on its own; it's only surfaced when it rides alongside a reorderEvents
	// that V2 drops.
	private sealed class ReorderEventsNoEffectOnV2Rule : IRule {
		public void Run(Script ast, KurrentDbVersion? quirksVersion, ProjectionVersion engineVersion, List<Diagnostic> diagnostics) {
			if (engineVersion != ProjectionVersion.V2)
				return;

			var scanner = new Scanner();
			scanner.Visit(ast);
			// A top-level user `options`/`function options` means the call isn't the builtin,
			// so the analysis can't be trusted - stay quiet. No reorderEvents means nothing to
			// warn about (lone processingLag is a no-op everywhere, not V2-specific).
			if (scanner.OptionsShadowed || scanner.ReorderEventsLocation is not { } reorderLoc)
				return;

			diagnostics.Add(DiagnosticCatalog.ReorderEventsNoEffectOnV2.ToDiagnostic(ToSourceRange(reorderLoc)));
			if (scanner.ProcessingLagLocation is { } lagLoc)
				diagnostics.Add(DiagnosticCatalog.ReorderEventsNoEffectOnV2.ToDiagnostic(ToSourceRange(lagLoc)));
		}

		// options is a top-level definition construct, so only calls and shadow declarations at
		// function depth 0 count; an options() in a handler body or nested/dead code is not the
		// source config and must be ignored.
		private sealed class Scanner : AstVisitor {
			private int _functionDepth;

			public bool OptionsShadowed { get; private set; }
			public Acornima.SourceLocation? ReorderEventsLocation { get; private set; }
			public Acornima.SourceLocation? ProcessingLagLocation { get; private set; }

			protected override object? VisitFunctionDeclaration(FunctionDeclaration node) {
				MarkShadow(node.Id);
				_functionDepth++;
				var result = base.VisitFunctionDeclaration(node);
				_functionDepth--;
				return result;
			}

			protected override object? VisitFunctionExpression(FunctionExpression node) {
				_functionDepth++;
				var result = base.VisitFunctionExpression(node);
				_functionDepth--;
				return result;
			}

			protected override object? VisitArrowFunctionExpression(ArrowFunctionExpression node) {
				_functionDepth++;
				var result = base.VisitArrowFunctionExpression(node);
				_functionDepth--;
				return result;
			}

			protected override object? VisitVariableDeclarator(VariableDeclarator node) {
				MarkShadow(node.Id);
				return base.VisitVariableDeclarator(node);
			}

			protected override object? VisitCallExpression(CallExpression node) {
				if (_functionDepth == 0 &&
					node.Callee is Identifier { Name: "options" } &&
					node.Arguments.Count > 0 &&
					node.Arguments[0] is ObjectExpression obj) {
					CollectReorderOptions(obj);
				}
				return base.VisitCallExpression(node);
			}

			private void MarkShadow(Node? id) {
				if (_functionDepth == 0 && id is Identifier { Name: "options" })
					OptionsShadowed = true;
			}

			private void CollectReorderOptions(ObjectExpression obj) {
				foreach (var p in obj.Properties) {
					if (p is not Property { Computed: false } prop)
						continue;
					var key = prop.Key switch {
						Identifier id => id.Name,
						StringLiteral lit => lit.Value,
						_ => null,
					};
					// An explicit `reorderEvents: false` is already off - nothing to warn about. A
					// non-literal value can't be proven off, so warn conservatively. Assigning on
					// every occurrence (clearing on false) keeps last-write-wins across duplicate
					// options() calls, so a later `false` suppresses an earlier `true`.
					if (key == "reorderEvents")
						ReorderEventsLocation = prop.Value is BooleanLiteral { Value: false } ? null : prop.Key.Location;
					else if (key == "processingLag")
						ProcessingLagLocation = prop.Key.Location;
				}
			}
		}
	}

	// See predicate-choice rationale on TransformsNotAppliedInV2Rule.
	private sealed class OutputStateUnconditionalInV2Rule : IRule {
		public void Run(Script ast, KurrentDbVersion? quirksVersion, ProjectionVersion engineVersion, List<Diagnostic> diagnostics) {
			if (engineVersion != ProjectionVersion.V2)
				return;

			// V2 always emits state to the result stream regardless of
			// outputState() (PartitionProcessor writes newState
			// unconditionally). The call succeeds but has no effect on
			// emission - flag as Information so the user knows it's redundant
			// without making it look like an error.
			var scanner = new MemberCallScanner("outputState");
			scanner.Visit(ast);
			foreach (var loc in scanner.Calls) {
				diagnostics.Add(DiagnosticCatalog.OutputStateUnconditional.ToDiagnostic(ToSourceRange(loc)));
			}
		}
	}

	// async / Promise-returning handlers silently produce empty state. The
	// projection engine (Jint) is synchronous with no event loop, so a handler
	// that returns a Promise has it serialized as the state - and a Promise has
	// no enumerable own properties, so the state becomes {} - rather than being
	// awaited. This matches KurrentDB but surprises users authoring tests in an
	// async-capable JS runtime, so warn at compile time.
	//
	// Scoped to the handler functions passed to when({...}) / chainHandlers({...}),
	// and only to a handler's *own* return value: an async helper or a Promise
	// returned from a function nested inside a handler doesn't determine the
	// handler's return, so flagging it would be misleading. The Promise check is a
	// literal-syntax heuristic (new Promise(...) / Promise.x(...)) and doesn't
	// account for a shadowed Promise global. Not quirk- or version-gated.
	private sealed class AsyncHandlerRule : IRule {
		public void Run(Script ast, KurrentDbVersion? quirksVersion, ProjectionVersion engineVersion, List<Diagnostic> diagnostics) {
			var scanner = new HandlerScanner();
			scanner.Visit(ast);

			foreach (var loc in scanner.AsyncHandlers) {
				diagnostics.Add(DiagnosticCatalog.HandlerAsync.ToDiagnostic(ToSourceRange(loc)));
			}
			foreach (var loc in scanner.PromiseReturns) {
				diagnostics.Add(DiagnosticCatalog.HandlerPromise.ToDiagnostic(ToSourceRange(loc)));
			}
		}

		private static bool IsPromiseConstruction(Node? expr) => expr switch {
			NewExpression { Callee: Identifier { Name: "Promise" } } => true,
			CallExpression { Callee: MemberExpression { Computed: false, Object: Identifier { Name: "Promise" } } } => true,
			_ => false,
		};

		// Finds when({...}) / chainHandlers({...}) calls and inspects each
		// handler-function value for an async modifier or a direct Promise return.
		private sealed class HandlerScanner : AstVisitor {
			public List<Acornima.SourceLocation> AsyncHandlers { get; } = new();
			public List<Acornima.SourceLocation> PromiseReturns { get; } = new();

			protected override object? VisitCallExpression(CallExpression node) {
				if (node.Callee is MemberExpression { Computed: false, Property: Identifier { Name: "when" or "chainHandlers" } } &&
					node.Arguments.Count > 0 &&
					node.Arguments[0] is ObjectExpression obj) {
					foreach (var p in obj.Properties) {
						if (p is Property { Computed: false } prop)
							AnalyzeHandler(prop.Value);
					}
				}
				return base.VisitCallExpression(node);
			}

			private void AnalyzeHandler(Node handler) {
				switch (handler) {
					case FunctionExpression fn:
						if (fn.Async)
							AsyncHandlers.Add(fn.Location);
						CollectDirectPromiseReturns(fn.Body);
						break;
					case ArrowFunctionExpression arrow:
						if (arrow.Async)
							AsyncHandlers.Add(arrow.Location);
						// Concise-body arrow `Ping: (s, e) => Promise.resolve(...)`
						// returns the body expression directly.
						if (arrow.Body is Expression body) {
							if (IsPromiseConstruction(body))
								PromiseReturns.Add(body.Location);
						} else {
							CollectDirectPromiseReturns(arrow.Body);
						}
						break;
				}
			}

			private void CollectDirectPromiseReturns(Node body) {
				var returns = new DirectReturnScanner();
				returns.Visit(body);
				PromiseReturns.AddRange(returns.PromiseReturns);
			}
		}

		// Collects Promise-returning return statements at the handler body's own
		// depth, descending into but ignoring returns inside nested functions
		// (those are some other function's return, not the handler's).
		private sealed class DirectReturnScanner : AstVisitor {
			private int _functionDepth;

			public List<Acornima.SourceLocation> PromiseReturns { get; } = new();

			protected override object? VisitFunctionDeclaration(FunctionDeclaration node) {
				_functionDepth++;
				var result = base.VisitFunctionDeclaration(node);
				_functionDepth--;
				return result;
			}

			protected override object? VisitFunctionExpression(FunctionExpression node) {
				_functionDepth++;
				var result = base.VisitFunctionExpression(node);
				_functionDepth--;
				return result;
			}

			protected override object? VisitArrowFunctionExpression(ArrowFunctionExpression node) {
				_functionDepth++;
				var result = base.VisitArrowFunctionExpression(node);
				_functionDepth--;
				return result;
			}

			protected override object? VisitReturnStatement(ReturnStatement node) {
				if (_functionDepth == 0 && node.Argument is { } arg && IsPromiseConstruction(arg))
					PromiseReturns.Add(arg.Location);
				return base.VisitReturnStatement(node);
			}
		}
	}
}
