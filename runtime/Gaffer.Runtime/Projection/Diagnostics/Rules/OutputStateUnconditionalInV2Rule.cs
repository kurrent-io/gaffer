using Acornima.Ast;
using Gaffer.Sdk.Diagnostics;
using Gaffer.Sdk.Versioning;

namespace Gaffer.Runtime.Projection.Diagnostics;

// See predicate-choice rationale on TransformsNotAppliedInV2Rule.
internal sealed class OutputStateUnconditionalInV2Rule : IRule {
	public void Run(Script ast, KurrentDbVersion? quirksVersion, ProjectionVersion engineVersion, List<Diagnostic> diagnostics) {
		if (engineVersion != ProjectionVersion.V2)
			return;

		// V2 always emits state to the result stream regardless of
		// outputState() (PartitionProcessor writes newState
		// unconditionally). The call succeeds but has no effect on
		// emission - flag as Information so the user knows it's redundant
		// without making it look like an error.
		var scanner = new MemberCallScanner("outputState");
		scanner.Visit(ast);
		foreach (var loc in scanner.Calls) {
			diagnostics.Add(DiagnosticCatalog.OutputStateUnconditional.ToDiagnostic(DiagnosticCollector.ToSourceRange(loc)));
		}
	}
}
