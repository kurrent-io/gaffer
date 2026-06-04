using Acornima.Ast;
using Gaffer.Sdk.Diagnostics;
using Gaffer.Sdk.Versioning;

namespace Gaffer.Runtime.Projection.Diagnostics;

// See predicate-choice rationale on TransformsNotAppliedInV2Rule.
internal sealed class OutputStateNoEffectOnV2Rule : IRule {
	public void Run(Script ast, KurrentDbVersion? quirksVersion, ProjectionVersion engineVersion, List<Diagnostic> diagnostics) {
		if (engineVersion != ProjectionVersion.V2
			|| !DiagnosticCatalog.OutputStateNoEffectOnV2.FiresAt(quirksVersion))
			return;

		// V2 doesn't emit result-stream events, so outputState() has no effect: a projection
		// relying on result-stream subscriptions gets nothing. Result-stream parity is planned
		// for a future release, so this is a version-gated quirk (fires for current versions).
		var scanner = new MemberCallScanner("outputState");
		scanner.Visit(ast);
		foreach (var loc in scanner.Calls) {
			diagnostics.Add(DiagnosticCatalog.OutputStateNoEffectOnV2.ToDiagnostic(DiagnosticCollector.ToSourceRange(loc)));
		}
	}
}
