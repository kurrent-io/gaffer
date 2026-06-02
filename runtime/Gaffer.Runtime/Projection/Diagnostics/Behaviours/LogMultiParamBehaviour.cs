using System.Text;
using Gaffer.Sdk.Diagnostics;
using Jint;
using Jint.Native;
using Jint.Native.Object;
using Jint.Runtime;

namespace Gaffer.Runtime.Projection.Diagnostics.Behaviours;

/// <summary>
/// Reproduces the multi-arg <c>log()</c> quirk. Upstream stacks three bugs: the separator gate
/// is <c>i &gt; 1</c> (so the first object has no leading separator), the separator string is
/// <c>" ,"</c>, and primitives are logged as separate lines instead of appended. The accumulated
/// object buffer is logged once at the end regardless. The two <c>if</c>s (not <c>if</c>/<c>else
/// if</c>) mirror upstream's structure; in Jint primitives and ObjectInstance are disjoint so it
/// makes no observable difference, but byte-fidelity is the goal.
/// </summary>
internal static class LogMultiParamBehaviour {
	public static void Apply(JsValue[] parameters, QuirkContext ctx) {
		// Surface the quirk at the point it fires (this log() call), so a runtime consumer sees
		// it inline. Also flagged at compile time.
		ctx.OnDiagnostic?.Invoke(DiagnosticCatalog.LogMultiParam.ToDiagnostic());
		var quirky = new StringBuilder();
		for (var i = 0; i < parameters.Length; i++) {
			if (i > 1)
				quirky.Append(" ,");
			var p = parameters.At(i);
			if (p != null && p.IsPrimitive())
				ctx.Log(p.ToString());
			if (p is ObjectInstance oi)
				quirky.Append(ctx.Serialize(oi));
		}
		ctx.Log(quirky.ToString());
	}
}
