using Gaffer.Sdk.Diagnostics;
using Gaffer.Sdk.Versioning;

namespace Gaffer.Runtime.Projection.Diagnostics;

// track_emitted_streams is unsupported on engine_version 2: V2 keeps no emitted-streams catalog, so
// the management layer rejects projection creation. Detected off the resolved QuerySources
// (authoritative - TrackEmittedStreams is set by options({trackEmittedStreams:true}) in the source
// or merged in from the gaffer.toml track_emitted_streams field), not the AST. An error diagnostic
// rather than a session throw, so info/dev/diff still compile and show the full analysis; deploy and
// recreate preflight refuse on the error severity before any write. Mirrors BiStateUnsupportedOnV2Rule.
internal sealed class TrackEmittedStreamsUnsupportedOnV2Rule : IDefinitionRule {
	public void Run(QuerySources definition, KurrentDbVersion? quirksVersion, ProjectionVersion engineVersion, List<Diagnostic> diagnostics) {
		if (engineVersion == ProjectionVersion.V2
			&& definition.TrackEmittedStreams
			&& DiagnosticCatalog.TrackEmittedStreamsUnsupportedOnV2.FiresAt(quirksVersion))
			diagnostics.Add(DiagnosticCatalog.TrackEmittedStreamsUnsupportedOnV2.ToDiagnostic());
	}
}
