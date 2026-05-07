using Acornima;
using Acornima.Ast;
using Gaffer.Sdk.Diagnostics;
using Gaffer.Sdk.Versioning;

namespace Gaffer.Runtime.Projection;

/// <summary>
/// Walks a projection's AST at compile time, running each registered
/// <see cref="IRule"/> and collecting any <see cref="Diagnostic"/>s they emit.
/// New rules plug in by adding to <see cref="Rules"/>.
/// </summary>
internal static class DiagnosticCollector {
	// Add new rules here. Each owns its own AST walk; UI-1543 (telemetry-
	// shaped rules) plug in alongside.
	private static readonly IRule[] Rules = new IRule[] {
		new LinkStreamToDeprecationRule(),
		new LinkStreamToOutOfBoundsParametersRule(),
		new LogMultiParamRule(),
		new TransformsNotAppliedInV2Rule(),
		new OutputStateImplicitInV2Rule(),
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
	public static Diagnostic[]? Scan(string source, KurrentDbVersion? dbVersion, ProjectionVersion engineVersion) {
		Script ast;
		try {
			ast = new Parser().ParseScript(source, "projection.js");
		} catch {
			return null;
		}
		var diagnostics = new List<Diagnostic>();
		foreach (var rule in Rules) {
			try {
				rule.Run(ast, dbVersion, engineVersion, diagnostics);
			} catch {
				// One rule failing doesn't taint the others.
			}
		}
		return diagnostics.Count > 0 ? diagnostics.ToArray() : null;
	}

	internal interface IRule {
		void Run(Script ast, KurrentDbVersion? dbVersion, ProjectionVersion engineVersion, List<Diagnostic> diagnostics);
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
		public void Run(Script ast, KurrentDbVersion? dbVersion, ProjectionVersion engineVersion, List<Diagnostic> diagnostics) {
			// Deprecation is independent of dbVersion - linkStreamTo is
			// undocumented at every released version we know about.
			var scanner = new IdentifierShadowScanner("linkStreamTo", _ => true);
			scanner.Visit(ast);
			if (scanner.Shadowed)
				return;

			foreach (var loc in scanner.Calls) {
				diagnostics.Add(new Diagnostic {
					Code = "deprecated.linkStreamTo",
					Message = "linkStreamTo is undocumented in KurrentDB and may be removed in a future version.",
					Severity = DiagnosticSeverity.Warning,
					Range = ToSourceRange(loc),
				});
			}
		}
	}

	private sealed class LinkStreamToOutOfBoundsParametersRule : IRule {
		public void Run(Script ast, KurrentDbVersion? dbVersion, ProjectionVersion engineVersion, List<Diagnostic> diagnostics) {
			if (!KnownBugs.LinkStreamToOutOfBoundsParameters.FiresAt(dbVersion))
				return;

			// 3+ args triggers the bug. 2-arg form is fine.
			var scanner = new IdentifierShadowScanner("linkStreamTo", call => call.Arguments.Count >= 3);
			scanner.Visit(ast);
			// Shadowed local linkStreamTo masks the upstream bug entirely -
			// the call goes to the user's function, not the buggy global.
			if (scanner.Shadowed)
				return;

			foreach (var loc in scanner.Calls) {
				diagnostics.Add(new Diagnostic {
					Code = KnownBugs.LinkStreamToOutOfBoundsParameters.Code,
					Message = "linkStreamTo with metadata (3+ args) crashes due to an upstream parameter-indexing bug; metadata is never captured.",
					Severity = DiagnosticSeverity.Warning,
					Range = ToSourceRange(loc),
				});
			}
		}
	}

	private sealed class LogMultiParamRule : IRule {
		public void Run(Script ast, KurrentDbVersion? dbVersion, ProjectionVersion engineVersion, List<Diagnostic> diagnostics) {
			if (!KnownBugs.LogMultiParam.FiresAt(dbVersion))
				return;

			// No shadow check: gaffer (and KurrentDB) registers `log` as a
			// non-configurable, non-writable global, so top-level
			// `var log = ...` / `function log() {}` collides at engine
			// initialisation and the projection won't even compile.
			// Inner-scope shadows are possible in theory but rare in practice.
			var scanner = new Scanner();
			scanner.Visit(ast);

			foreach (var loc in scanner.ProblematicCalls) {
				diagnostics.Add(new Diagnostic {
					Code = KnownBugs.LogMultiParam.Code,
					Message = "log() with multiple args produces unexpected output due to an upstream bug: primitives become separate log lines and objects use a ' ,' separator.",
					Severity = DiagnosticSeverity.Warning,
					Range = ToSourceRange(loc),
				});
			}
		}

		private sealed class Scanner : AstVisitor {
			public readonly List<Acornima.SourceLocation> ProblematicCalls = new();

			protected override object? VisitCallExpression(CallExpression node) {
				// 2+ args triggers the upstream multi-param bug. 1-arg path is fine.
				if (node.Callee is Identifier { Name: "log" } id && node.Arguments.Count >= 2)
					ProblematicCalls.Add(id.Location);
				return base.VisitCallExpression(node);
			}
		}
	}

	/// <summary>
	/// Scans for chained method calls of a named property (e.g. <c>x.foo()</c>),
	/// optionally suppressed when a top-level identifier of the same name is
	/// declared. Used for transforms/outputState which are chain methods on
	/// the projection runtime, not globals.
	/// </summary>
	private sealed class MemberCallScanner : AstVisitor {
		private readonly string _name;

		public MemberCallScanner(string name) {
			_name = name;
		}

		public bool Shadowed { get; private set; }
		public List<Acornima.SourceLocation> Calls { get; } = new();

		protected override object? VisitVariableDeclarator(VariableDeclarator node) {
			// Top-level local with the same name suggests the user is
			// rebinding the global - suppress to avoid false positives.
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
			if (node.Callee is MemberExpression me &&
				!me.Computed &&
				me.Property is Identifier { Name: var propName } prop &&
				propName == _name) {
				Calls.Add(prop.Location);
			}
			return base.VisitCallExpression(node);
		}
	}

	private sealed class TransformsNotAppliedInV2Rule : IRule {
		public void Run(Script ast, KurrentDbVersion? dbVersion, ProjectionVersion engineVersion, List<Diagnostic> diagnostics) {
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
			if (scanner.Shadowed)
				return;
			foreach (var loc in scanner.Calls) {
				diagnostics.Add(new Diagnostic {
					Code = "compat.transforms.notApplied",
					Message = $"{name}() is registered but never invoked under engine_version=2; result equals post-handler state. See v1-v2-differences.",
					Severity = DiagnosticSeverity.Warning,
					Range = ToSourceRange(loc),
				});
			}
		}
	}

	private sealed class OutputStateImplicitInV2Rule : IRule {
		public void Run(Script ast, KurrentDbVersion? dbVersion, ProjectionVersion engineVersion, List<Diagnostic> diagnostics) {
			if (engineVersion != ProjectionVersion.V2)
				return;

			// V2 always emits state to the result stream regardless of
			// outputState() (PartitionProcessor writes newState
			// unconditionally). The call succeeds but has no effect on
			// emission - flag as a Hint so the user knows it's redundant
			// without making it look like an error.
			var scanner = new MemberCallScanner("outputState");
			scanner.Visit(ast);
			if (scanner.Shadowed)
				return;
			foreach (var loc in scanner.Calls) {
				diagnostics.Add(new Diagnostic {
					Code = "compat.outputState.unconditional",
					Message = "outputState() has no effect under engine_version=2; state is always emitted to the result stream. See v1-v2-differences.",
					Severity = DiagnosticSeverity.Hint,
					Range = ToSourceRange(loc),
				});
			}
		}
	}
}
