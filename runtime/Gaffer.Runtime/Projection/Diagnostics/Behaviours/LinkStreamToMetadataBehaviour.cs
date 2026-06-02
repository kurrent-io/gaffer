using Jint;
using Jint.Native;
using Jint.Runtime;

namespace Gaffer.Runtime.Projection.Diagnostics.Behaviours;

/// <summary>
/// Reproduces the <c>linkStreamTo(stream, link, metadata)</c> out-of-bounds-parameter quirk:
/// the metadata branch reads <c>parameters.At(4)</c> while the arity gate is length 3, so on a
/// 3-arg call index 4 is out of bounds, returns Undefined, and <c>AsObject()</c> throws the
/// byte-identical exception KurrentDB raises. The caller wraps this in the handler's
/// compat-code stash so the thrown error carries the quirk code.
/// </summary>
internal static class LinkStreamToMetadataBehaviour {
	public static Dictionary<string, string?> Apply(JsValue[] parameters, QuirkContext ctx) {
		var md = parameters.At(4).AsObject();
		var metadata = new Dictionary<string, string?>();
		foreach (var kvp in md.GetOwnProperties())
			metadata.Add(kvp.Key.AsString(), ctx.AsString(kvp.Value.Value, false));
		return metadata;
	}
}
