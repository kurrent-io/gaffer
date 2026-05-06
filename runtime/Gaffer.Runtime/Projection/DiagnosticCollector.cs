using Acornima;
using Acornima.Ast;
using Gaffer.Sdk.Diagnostics;

namespace Gaffer.Runtime.Projection;

/// <summary>
/// Walks a projection's AST at compile time, running each registered
/// <see cref="IRule"/> and collecting any <see cref="Diagnostic"/>s they emit.
/// New rules plug in by adding to <see cref="Rules"/>.
/// </summary>
internal static class DiagnosticCollector {
	// Add new rules here. Each owns its own AST walk; UI-1527 (compat
	// warnings) and UI-1543 (telemetry-shaped rules) plug in alongside.
	private static readonly IRule[] Rules = new IRule[] {
		new LinkStreamToDeprecationRule(),
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
	public static Diagnostic[]? Scan(string source) {
		Script ast;
		try {
			ast = new Parser().ParseScript(source, "projection.js");
		} catch {
			return null;
		}
		var diagnostics = new List<Diagnostic>();
		foreach (var rule in Rules) {
			try {
				rule.Run(ast, diagnostics);
			} catch {
				// One rule failing doesn't taint the others.
			}
		}
		return diagnostics.Count > 0 ? diagnostics.ToArray() : null;
	}

	internal interface IRule {
		void Run(Script ast, List<Diagnostic> diagnostics);
	}

	// Acornima: line 1-based, column 0-based. Sdk: both 1-based.
	// Acornima.SourceLocation fully qualified to avoid confusion with our
	// SourceRange/SourcePosition.
	internal static SourceRange ToSourceRange(Acornima.SourceLocation loc) => new() {
		Start = new SourcePosition { Line = loc.Start.Line, Column = loc.Start.Column + 1 },
		End = new SourcePosition { Line = loc.End.Line, Column = loc.End.Column + 1 },
	};

	private sealed class LinkStreamToDeprecationRule : IRule {
		public void Run(Script ast, List<Diagnostic> diagnostics) {
			// Two-pass: first detect whether the projection shadows the
			// runtime-provided `linkStreamTo` (var/let/const/function with
			// that name). If so, suppress - the call we'd flag isn't the
			// deprecated global.
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
				// var/let/const linkStreamTo = ...
				if (node.Id is Identifier { Name: "linkStreamTo" })
					Shadowed = true;
				return base.VisitVariableDeclarator(node);
			}

			protected override object? VisitFunctionDeclaration(FunctionDeclaration node) {
				// function linkStreamTo() {}
				if (node.Id is Identifier { Name: "linkStreamTo" })
					Shadowed = true;
				return base.VisitFunctionDeclaration(node);
			}

			protected override object? VisitCallExpression(CallExpression node) {
				// Identifier callee only - we deliberately don't match
				// member access (`obj.linkStreamTo()`) or indirect calls
				// (`(0, linkStreamTo)()`). KurrentDB binds linkStreamTo
				// as a global; users virtually always call it directly,
				// and KurrentDB itself can't resolve the indirect forms
				// either.
				if (node.Callee is Identifier { Name: "linkStreamTo" } id)
					Calls.Add(id.Location);
				return base.VisitCallExpression(node);
			}
		}
	}
}
