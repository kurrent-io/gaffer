namespace Gaffer.Sdk.Diagnostics;

/// <summary>
/// A diagnostic surfaced at projection compile time.
/// <para>
/// <see cref="Code"/> is namespaced as <c>&lt;category&gt;.&lt;name&gt;</c>
/// (e.g. <c>deprecated.linkStreamTo</c>, <c>compat.log.multiParam</c>) so
/// consumers can filter, group, or render by category without parsing messages.
/// </para>
/// </summary>
public sealed class Diagnostic {
	public required string Code { get; init; }
	public required string Message { get; init; }
	public required DiagnosticSeverity Severity { get; init; }
	public SourceRange? Range { get; init; }
}

/// <summary>
/// Severity of a <see cref="Diagnostic"/>. Values match the LSP
/// <c>DiagnosticSeverity</c> enum so editor adapters can pass them through.
/// </summary>
public enum DiagnosticSeverity {
	Error = 1,
	Warning = 2,
	Information = 3,
	Hint = 4,
}
