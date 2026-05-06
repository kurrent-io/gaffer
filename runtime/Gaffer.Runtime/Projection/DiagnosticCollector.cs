using Acornima;
using Acornima.Ast;
using Gaffer.Sdk;

namespace Gaffer.Runtime.Projection;

/// <summary>
/// Walks a projection's AST at compile time looking for patterns that should
/// surface as <see cref="Diagnostic"/>s on <see cref="Sdk.ProjectionInfo"/>.
/// </summary>
internal static class DiagnosticCollector {
	/// <summary>
	/// Parse <paramref name="source"/> with Acornima and return diagnostics, or
	/// <c>null</c> if there are none. Caller must have validated the source
	/// parses (typically by constructing a <see cref="JintProjectionHandler"/>
	/// first); this re-parses with default options to walk the AST.
	/// <para>
	/// Returns <c>null</c> on parse failure - if Acornima rejects source that
	/// Jint accepted (option drift), we drop the diagnostics rather than break
	/// an otherwise-valid session. Diagnostics are best-effort.
	/// </para>
	/// </summary>
	public static Diagnostic[]? Scan(string source) {
		Script ast;
		try {
			ast = new Parser().ParseScript(source, "projection.js");
		} catch (ParseErrorException) {
			return null;
		}
		var visitor = new Visitor();
		visitor.Visit(ast);
		return visitor.Diagnostics.Count > 0 ? visitor.Diagnostics.ToArray() : null;
	}

	private sealed class Visitor : AstVisitor {
		public readonly List<Diagnostic> Diagnostics = new();

		protected override object? VisitCallExpression(CallExpression node) {
			// Identifier callee only - we deliberately don't match member access
			// (`obj.linkStreamTo()`) or indirect calls (`(0, linkStreamTo)()`).
			// KurrentDB binds linkStreamTo as a global; users virtually always
			// call it directly, and KurrentDB itself can't statically resolve
			// the indirect forms either.
			if (node.Callee is Identifier { Name: "linkStreamTo" } id) {
				Diagnostics.Add(new Diagnostic {
					Code = "deprecated.linkStreamTo",
					Message = "linkStreamTo is undocumented in KurrentDB and may be removed in a future version.",
					Severity = DiagnosticSeverity.Warning,
					Range = ToSourceRange(id.Location),
				});
			}
			return base.VisitCallExpression(node);
		}

		// Acornima: line 1-based, column 0-based. Sdk: both 1-based.
		private static SourceRange ToSourceRange(SourceLocation loc) => new() {
			Start = new SourcePosition { Line = loc.Start.Line, Column = loc.Start.Column + 1 },
			End = new SourcePosition { Line = loc.End.Line, Column = loc.End.Column + 1 },
		};
	}
}
