using Gaffer.Sdk.Diagnostics;
using Jint.Native;

namespace Gaffer.Runtime.Projection.Diagnostics.Behaviours;

/// <summary>
/// The handler capabilities a runtime quirk behaviour borrows: the diagnostic sink plus the
/// value-formatting helpers it needs to reproduce upstream output. Built once per handler and
/// passed to each behaviour's <c>Apply</c>, so a quirk's reproduction logic lives co-located
/// with its <see cref="DiagnosticDescriptor"/> rather than inline in the handler. The handler is
/// left with uniform <c>FiresAt? -&gt; Apply</c> dispatch.
/// </summary>
internal sealed class QuirkContext {
	/// <summary>Per-event diagnostic sink, invoked at the point a quirk fires.</summary>
	public Action<Diagnostic>? OnDiagnostic { get; init; }

	/// <summary>Persist a value as its state string, mapping null/undefined to null (the handler's ConvertToStringHandlingNulls).</summary>
	public required Func<JsValue, string?> ToPersistedString { get; init; }

	/// <summary>Serialize a value to JSON (the handler's Serialize).</summary>
	public required Func<JsValue, string> Serialize { get; init; }

	/// <summary>Emit a single log line (the handler's SafeInvokeLog).</summary>
	public required Action<string> Log { get; init; }

	/// <summary>Stringify a value for emitted-event payloads (the handler's AsString).</summary>
	public required Func<JsValue?, bool, string?> AsString { get; init; }
}
