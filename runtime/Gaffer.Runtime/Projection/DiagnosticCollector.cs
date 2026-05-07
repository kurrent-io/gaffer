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
	public static Diagnostic[]? Scan(string source, KurrentDbVersion? dbVersion) {
		Script ast;
		try {
			ast = new Parser().ParseScript(source, "projection.js");
		} catch {
			return null;
		}
		var diagnostics = new List<Diagnostic>();
		foreach (var rule in Rules) {
			try {
				rule.Run(ast, dbVersion, diagnostics);
			} catch {
				// One rule failing doesn't taint the others.
			}
		}
		return diagnostics.Count > 0 ? diagnostics.ToArray() : null;
	}

	internal interface IRule {
		void Run(Script ast, KurrentDbVersion? dbVersion, List<Diagnostic> diagnostics);
	}

	// Acornima: line 1-based, column 0-based. Sdk: both 1-based.
	// Acornima.SourceLocation fully qualified to avoid confusion with our
	// SourceRange/SourcePosition.
	internal static SourceRange ToSourceRange(Acornima.SourceLocation loc) => new() {
		Start = new SourcePosition { Line = loc.Start.Line, Column = loc.Start.Column + 1 },
		End = new SourcePosition { Line = loc.End.Line, Column = loc.End.Column + 1 },
	};

	private sealed class LinkStreamToDeprecationRule : IRule {
		public void Run(Script ast, KurrentDbVersion? dbVersion, List<Diagnostic> diagnostics) {
			// Deprecation is independent of dbVersion - linkStreamTo is
			// undocumented at every released version we know about.
			var scanner = new Scanner();
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

		private sealed class Scanner : AstVisitor {
			public bool Shadowed;
			public readonly List<Acornima.SourceLocation> Calls = new();

			protected override object? VisitVariableDeclarator(VariableDeclarator node) {
				if (node.Id is Identifier { Name: "linkStreamTo" })
					Shadowed = true;
				return base.VisitVariableDeclarator(node);
			}

			protected override object? VisitFunctionDeclaration(FunctionDeclaration node) {
				if (node.Id is Identifier { Name: "linkStreamTo" })
					Shadowed = true;
				return base.VisitFunctionDeclaration(node);
			}

			protected override object? VisitCallExpression(CallExpression node) {
				if (node.Callee is Identifier { Name: "linkStreamTo" } id)
					Calls.Add(id.Location);
				return base.VisitCallExpression(node);
			}
		}
	}

	private sealed class LinkStreamToOutOfBoundsParametersRule : IRule {
		public void Run(Script ast, KurrentDbVersion? dbVersion, List<Diagnostic> diagnostics) {
			if (!KnownBugs.LinkStreamToOutOfBoundsParameters.FiresAt(dbVersion))
				return;

			var scanner = new Scanner();
			scanner.Visit(ast);
			// Shadowed local linkStreamTo masks the upstream bug entirely -
			// the call goes to the user's function, not the buggy global.
			if (scanner.Shadowed)
				return;

			foreach (var loc in scanner.ProblematicCalls) {
				diagnostics.Add(new Diagnostic {
					Code = KnownBugs.LinkStreamToOutOfBoundsParameters.Code,
					Message = "linkStreamTo with metadata (3+ args) crashes due to an upstream parameter-indexing bug; metadata is never captured.",
					Severity = DiagnosticSeverity.Warning,
					Range = ToSourceRange(loc),
				});
			}
		}

		private sealed class Scanner : AstVisitor {
			public bool Shadowed;
			public readonly List<Acornima.SourceLocation> ProblematicCalls = new();

			protected override object? VisitVariableDeclarator(VariableDeclarator node) {
				if (node.Id is Identifier { Name: "linkStreamTo" })
					Shadowed = true;
				return base.VisitVariableDeclarator(node);
			}

			protected override object? VisitFunctionDeclaration(FunctionDeclaration node) {
				if (node.Id is Identifier { Name: "linkStreamTo" })
					Shadowed = true;
				return base.VisitFunctionDeclaration(node);
			}

			protected override object? VisitCallExpression(CallExpression node) {
				// 3+ args triggers the bug. 2-arg form is fine.
				if (node.Callee is Identifier { Name: "linkStreamTo" } id && node.Arguments.Count >= 3)
					ProblematicCalls.Add(id.Location);
				return base.VisitCallExpression(node);
			}
		}
	}

	private sealed class LogMultiParamRule : IRule {
		public void Run(Script ast, KurrentDbVersion? dbVersion, List<Diagnostic> diagnostics) {
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
}
