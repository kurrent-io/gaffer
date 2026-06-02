using Acornima.Ast;
using Gaffer.Sdk.Diagnostics;
using Gaffer.Sdk.Versioning;

namespace Gaffer.Runtime.Projection.Diagnostics;

internal sealed class LinkStreamToDeprecationRule : IRule {
	public void Run(Script ast, KurrentDbVersion? quirksVersion, ProjectionVersion engineVersion, List<Diagnostic> diagnostics) {
		// Deprecation is independent of quirksVersion - linkStreamTo is
		// undocumented at every released version we know about.
		var scanner = new IdentifierShadowScanner("linkStreamTo", _ => true);
		scanner.Visit(ast);
		if (scanner.Shadowed)
			return;

		foreach (var loc in scanner.Calls) {
			diagnostics.Add(DiagnosticCatalog.LinkStreamToDeprecated.ToDiagnostic(DiagnosticCollector.ToSourceRange(loc)));
		}
	}
}
