using Acornima;
using Acornima.Ast;

namespace Gaffer.Runtime.Projection;

/// <summary>
/// Detects whether a projection writes events: whether it calls one of the
/// in-handler write sinks <c>emit()</c>, <c>linkTo()</c>,
/// <c>linkStreamTo()</c>, or <c>copyTo()</c> as a bare identifier.
/// <para>
/// Runs on the AST already parsed for diagnostics
/// (<see cref="Diagnostics.DiagnosticCollector"/>), so emit-ness is known on
/// every compile without the <c>IncludeShape</c> shape walk. The result is
/// surfaced as <see cref="Gaffer.Sdk.ProjectionInfo.EmitsEvents"/> and is the
/// authoritative signal deploy uses to set the server's <c>emitEnabled</c>.
/// </para>
/// </summary>
internal static class EmitDetector {
	// Bare-identifier write sinks - the single source of truth for "which
	// builtins write events". <see cref="ShapeCollector"/> composes this set
	// (it counts the same sinks alongside the source builtins), so a new sink
	// added here is picked up by both. A member call (`someService.emit(...)`)
	// does NOT count: Identifier-only dispatch.
	internal static readonly HashSet<string> WriteSinks = new() {
		"emit", "linkTo", "linkStreamTo", "copyTo",
	};

	public static bool Detect(Script ast) {
		var scanner = new Scanner();
		scanner.Visit(ast);
		return scanner.Found;
	}

	private sealed class Scanner : AstVisitor {
		public bool Found { get; private set; }

		protected override object? VisitCallExpression(CallExpression node) {
			// Stop descending once a sink is found; siblings still get visited
			// but return immediately, so the walk stays cheap.
			if (Found) {
				return node;
			}
			if (node.Callee is Identifier id && WriteSinks.Contains(id.Name)) {
				Found = true;
				return node;
			}
			return base.VisitCallExpression(node);
		}
	}
}
