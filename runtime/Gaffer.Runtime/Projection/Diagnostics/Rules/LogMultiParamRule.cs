using Acornima;
using Acornima.Ast;
using Gaffer.Sdk.Diagnostics;
using Gaffer.Sdk.Versioning;

namespace Gaffer.Runtime.Projection.Diagnostics;

internal sealed class LogMultiParamRule : IRule {
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
			diagnostics.Add(DiagnosticCatalog.LogMultiParam.ToDiagnostic(DiagnosticCollector.ToSourceRange(loc)));
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
