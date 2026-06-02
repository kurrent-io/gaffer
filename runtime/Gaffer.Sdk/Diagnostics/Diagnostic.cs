namespace Gaffer.Sdk.Diagnostics;

/// <summary>
/// A diagnostic surfaced at projection compile time or runtime.
/// <para>
/// <see cref="Code"/> is namespaced as <c>&lt;class&gt;.&lt;subject&gt;.&lt;detail&gt;</c>
/// (e.g. <c>quirk.log.multiParam</c>, <c>usage.handler.async</c>) so consumers can filter,
/// group, or render by class without parsing messages. See <see cref="DiagnosticCatalog"/>
/// for the full set.
/// </para>
/// </summary>
public sealed class Diagnostic {
	public required string Code { get; init; }
	public required string Message { get; init; }
	public required DiagnosticSeverity Severity { get; init; }
	public SourceRange? Range { get; init; }
}

/// <summary>
/// Severity of a <see cref="Diagnostic"/>. Values match the LSP <c>DiagnosticSeverity</c>
/// enum so editor adapters can pass them through. <c>Hint</c> (4) is intentionally omitted -
/// gaffer emits only Error/Warning/Information.
/// </summary>
public enum DiagnosticSeverity {
	Error = 1,
	Warning = 2,
	Information = 3,
}
