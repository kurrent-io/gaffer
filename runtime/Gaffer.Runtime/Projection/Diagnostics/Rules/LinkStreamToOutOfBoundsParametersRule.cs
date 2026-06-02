using Acornima.Ast;
using Gaffer.Sdk.Diagnostics;
using Gaffer.Sdk.Versioning;

namespace Gaffer.Runtime.Projection.Diagnostics;

internal sealed class LinkStreamToOutOfBoundsParametersRule : IRule {
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
			diagnostics.Add(DiagnosticCatalog.LinkStreamToOutOfBoundsParameters.ToDiagnostic(DiagnosticCollector.ToSourceRange(loc)));
		}
	}
}
