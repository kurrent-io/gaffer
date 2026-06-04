using Gaffer.Sdk.Diagnostics;
using Gaffer.Sdk.Versioning;

namespace Gaffer.Runtime.Projection.Diagnostics;

// Bi-state / $initShared is unsupported on engine_version 2: the engine silently re-initializes
// shared state on restart instead of restoring it, producing wrong results. Detected off the
// resolved QuerySources (authoritative - IsBiState is set by options({biState:true}) or a declared
// $initShared), not the AST. gaffer can't reproduce the restart, so this is a compile-time warning.
internal sealed class BiStateUnsupportedOnV2Rule : IDefinitionRule {
	public void Run(QuerySources definition, KurrentDbVersion? quirksVersion, ProjectionVersion engineVersion, List<Diagnostic> diagnostics) {
		if (engineVersion == ProjectionVersion.V2
			&& definition.IsBiState
			&& DiagnosticCatalog.BiStateSharedStateResetOnV2.FiresAt(quirksVersion))
			diagnostics.Add(DiagnosticCatalog.BiStateSharedStateResetOnV2.ToDiagnostic());
	}
}
