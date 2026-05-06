using Acornima;
using Acornima.Ast;
using Gaffer.Sdk.Diagnostics;

namespace Gaffer.Runtime.Projection;

/// <summary>
/// Walks a projection's AST at compile time, running each registered
/// <see cref="IRule"/> and collecting any <see cref="Diagnostic"/>s they emit.
/// One parse, one walk - new rules plug in by adding to <see cref="Rules"/>.
/// </summary>
internal static class DiagnosticCollector {
	// Add new rules here. They share the AST walk and a single output list.
	// UI-1527 (compat warnings) and UI-1543 (telemetry-shaped rules) will
	// plug in alongside.
	private static readonly IRule[] Rules = new IRule[] {
		new LinkStreamToDeprecationRule(),
	};

	/// <summary>
	/// Parse <paramref name="source"/> with Acornima and run every rule. Returns
	/// the collected diagnostics, or <c>null</c> if there are none.
	/// <para>
	/// Returns <c>null</c> on parse failure too - if Acornima rejects source
	/// that Jint accepted (option drift), we drop the diagnostics rather than
	/// break an otherwise-valid session. Diagnostics are best-effort.
	/// </para>
	/// </summary>
	public static Diagnostic[]? Scan(string source) {
		Script ast;
		try {
			ast = new Parser().ParseScript(source, "projection.js");
		} catch (ParseErrorException) {
			return null;
		}
		var diagnostics = new List<Diagnostic>();
		new RuleVisitor(Rules, diagnostics).Visit(ast);
		return diagnostics.Count > 0 ? diagnostics.ToArray() : null;
	}

	internal interface IRule {
		void OnNode(Node node, List<Diagnostic> diagnostics);
	}

	private sealed class RuleVisitor(IRule[] rules, List<Diagnostic> diagnostics) : AstVisitor {
		public override object? Visit(Node node) {
			foreach (var rule in rules)
				rule.OnNode(node, diagnostics);
			return base.Visit(node);
		}
	}

	// Acornima: line 1-based, column 0-based. Sdk: both 1-based.
	// Acornima.SourceLocation fully qualified to avoid confusing the reader -
	// SourceRange and SourcePosition are ours, the parameter is Acornima's.
	internal static SourceRange ToSourceRange(Acornima.SourceLocation loc) => new() {
		Start = new SourcePosition { Line = loc.Start.Line, Column = loc.Start.Column + 1 },
		End = new SourcePosition { Line = loc.End.Line, Column = loc.End.Column + 1 },
	};

	private sealed class LinkStreamToDeprecationRule : IRule {
		public void OnNode(Node node, List<Diagnostic> diagnostics) {
			// Identifier callee only - we deliberately don't match member access
			// (`obj.linkStreamTo()`) or indirect calls (`(0, linkStreamTo)()`).
			// KurrentDB binds linkStreamTo as a global; users virtually always
			// call it directly, and KurrentDB itself can't statically resolve
			// the indirect forms either.
			if (node is CallExpression { Callee: Identifier { Name: "linkStreamTo" } id }) {
				diagnostics.Add(new Diagnostic {
					Code = "deprecated.linkStreamTo",
					Message = "linkStreamTo is undocumented in KurrentDB and may be removed in a future version.",
					Severity = DiagnosticSeverity.Warning,
					Range = ToSourceRange(id.Location),
				});
			}
		}
	}
}
