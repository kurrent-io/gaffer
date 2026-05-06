namespace Gaffer.Sdk;

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
