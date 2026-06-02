using Gaffer.Runtime.Errors;
using Gaffer.Sdk.Diagnostics;

namespace Gaffer.Runtime.Projection.Diagnostics.Behaviours;

/// <summary>
/// Reproduces the non-finite serialization quirk: <c>Utf8JsonWriter</c> rejects
/// <c>NaN</c>/<c>Infinity</c> (JSON has no representation for them), so upstream throws. The
/// clean (post-fix) path writes JSON <c>null</c> instead. The thrown exception carries the quirk
/// code via <c>CompatCode</c>.
/// </summary>
internal static class SerializeNonFiniteBehaviour {
	public static void Throw(double value) =>
		throw new StateSerializationException($"{value} is not a valid JSON value", "", "", 0) {
			CompatCode = DiagnosticCatalog.SerializeNonFinite.Code,
		};
}
