using Gaffer.Sdk.Diagnostics;
using Jint.Native;

namespace Gaffer.Runtime.Projection.Diagnostics.Behaviours;

/// <summary>
/// Detects the uni-state raw-string quirk: pre-26.2.0 the engine persists a bare string state
/// un-encoded (e.g. <c>hello</c> rather than <c>"hello"</c>), so a reload's <c>JsonParser.Parse</c>
/// faults. gaffer can't simulate the reload, and emitting the raw string would break its own
/// result serialization, so it JSON-encodes the state (matching the fix) and reports the quirk to
/// warn that the projection would fault on reload against a pre-26.2.0 engine.
/// </summary>
internal static class RawStringStateBehaviour {
	public static string? Apply(JsValue state, bool reportQuirks, QuirkContext ctx) {
		if (reportQuirks)
			ctx.OnDiagnostic?.Invoke(DiagnosticCatalog.SerializeRawString.ToDiagnostic());
		return ctx.ToPersistedString(state);
	}
}
