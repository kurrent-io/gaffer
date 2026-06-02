using Gaffer.Sdk.Diagnostics;
using Jint;
using Jint.Native;

namespace Gaffer.Runtime.Projection.Diagnostics.Behaviours;

/// <summary>
/// Reproduces the bi-state string-slot quirk: a raw string written to a bi-state state-array
/// slot is JSON-quoted (double-encoded) on persistence instead of passed through, because
/// upstream checks the array's type rather than the element's. Each slot (partition / shared)
/// has its own <see cref="DiagnosticDescriptor"/> since PR #5610 only fixed slot 0. Surfaced as
/// a runtime diagnostic so a test can catch the double-quoting.
/// </summary>
internal static class BiStateSlotBehaviour {
	/// <summary>The quirky branch: report (when this is the state-pass) and double-encode the slot.</summary>
	public static string? Apply(JsValue slot, DiagnosticDescriptor quirk, bool reportQuirks, QuirkContext ctx) {
		if (reportQuirks && slot.IsString())
			ctx.OnDiagnostic?.Invoke(quirk.ToDiagnostic());
		return ctx.ToPersistedString(slot);
	}
}
