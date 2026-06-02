using Acornima.Ast;
using Gaffer.Sdk.Diagnostics;
using Gaffer.Sdk.Versioning;

namespace Gaffer.Runtime.Projection.Diagnostics;

// Predicate is `== V2` rather than `<= V2.x`: when V2 grows transforms
// in some future engine version, the rule should stop firing for that
// version, not start firing for *future* versions before they exist.
// Re-evaluate this gate when a third engine version lands.
internal sealed class TransformsNotAppliedInV2Rule : IRule {
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
			diagnostics.Add(DiagnosticCatalog.TransformsNotInvoked.ToDiagnostic(DiagnosticCollector.ToSourceRange(loc)));
		}
	}
}
