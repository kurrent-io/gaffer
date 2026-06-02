using Jint.Native;
using Jint.Native.Object;

namespace Gaffer.Runtime.Projection.Diagnostics.Behaviours;

/// <summary>
/// Reproduces the <c>event.body</c> cast quirk: upstream's out parameter is typed
/// <c>ObjectInstance</c>, forcing a cast that throws <c>InvalidCastException</c> when the body is
/// null, a number, a string, or a boolean. The caller wraps this in the handler's compat-code
/// stash so the thrown error carries the quirk code.
/// </summary>
internal static class EventBodyCastBehaviour {
	public static JsValue Apply(JsValue body) => (ObjectInstance)body;
}
