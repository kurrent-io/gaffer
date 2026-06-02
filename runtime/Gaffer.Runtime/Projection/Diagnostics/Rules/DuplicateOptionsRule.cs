using Acornima.Ast;
using Gaffer.Sdk.Diagnostics;
using Gaffer.Sdk.Versioning;

namespace Gaffer.Runtime.Projection.Diagnostics;

// options() is last-write-wins: a second call silently discards the
// first. That's almost always a refactor mistake (a stale options
// block left behind), so warn on every call past the first. Not
// quirk- or version-gated - it's a usage lint, true at all versions.
internal sealed class DuplicateOptionsRule : IRule {
	public void Run(Script ast, KurrentDbVersion? quirksVersion, ProjectionVersion engineVersion, List<Diagnostic> diagnostics) {
		var scanner = new IdentifierShadowScanner("options", _ => true);
		scanner.Visit(ast);
		// A top-level `var options` / `function options` shadows the
		// definition global, so these calls aren't the real options().
		if (scanner.Shadowed || scanner.Calls.Count <= 1)
			return;

		// Skip the first call; flag each later one as the duplicate.
		foreach (var loc in scanner.Calls.Skip(1)) {
			diagnostics.Add(DiagnosticCatalog.OptionsDuplicate.ToDiagnostic(DiagnosticCollector.ToSourceRange(loc)));
		}
	}
}
