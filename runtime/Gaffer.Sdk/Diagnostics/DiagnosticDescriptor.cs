using Gaffer.Sdk.Versioning;

namespace Gaffer.Sdk.Diagnostics;

/// <summary>
/// The definition of one diagnostic: its stable <see cref="Code"/>, classification, default
/// severity, message, and docs. The single source of truth - the compile-time rules and the
/// runtime quirk behaviours both reference their descriptor rather than inlining codes,
/// messages, or severities.
/// </summary>
public sealed record DiagnosticDescriptor {
	/// <summary>Stable, namespaced code: <c>&lt;class&gt;.&lt;subject&gt;.&lt;detail&gt;</c>.</summary>
	public required string Code { get; init; }

	public required DiagnosticClass Class { get; init; }

	/// <summary>
	/// Default severity, per the per-firing rubric: <c>Error</c> when there is no correct form
	/// (the construct throws or is unsupported), <c>Warning</c> when it runs but produces a
	/// wrong/surprising result, <c>Information</c> when it works but is noteworthy
	/// (lifecycle/redundancy).
	/// </summary>
	public required DiagnosticSeverity Severity { get; init; }

	/// <summary>The human-facing one-line message.</summary>
	public required string Message { get; init; }

	/// <summary>Longer explanation (Markdown) for docs surfaces. Optional.</summary>
	public string? Docs { get; init; }

	/// <summary>
	/// Quirks only: the KurrentDB version that fixes this upstream. <c>null</c> = no fix in
	/// flight, so the quirk fires in every configuration.
	/// </summary>
	public KurrentDbVersion? FixedIn { get; init; }

	/// <summary>
	/// Quirks only: whether the quirk has a runtime behaviour reproduced in the handler (some
	/// also have a compile-time rule). Marks which descriptors have a runtime behaviour to
	/// dispatch to.
	/// </summary>
	public bool HasRuntimeBehaviour { get; init; }

	/// <summary>
	/// Quirks only: true when this quirk should be reproduced for the given session quirks
	/// version. An unset version (unversioned, "all quirks on") and a quirk with no fix in
	/// flight both fire.
	/// </summary>
	public bool FiresAt(KurrentDbVersion? quirksVersion) =>
		quirksVersion is null || FixedIn is null || quirksVersion < FixedIn;

	/// <summary>Build a <see cref="Diagnostic"/> from this descriptor at an optional source range.</summary>
	public Diagnostic ToDiagnostic(SourceRange? range = null) => new() {
		Code = Code,
		Message = Message,
		Severity = Severity,
		Range = range,
	};
}
