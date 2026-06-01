using Gaffer.Sdk.Diagnostics;

namespace Gaffer.Runtime.Errors;

public abstract class ProjectionException : Exception {
	public abstract string Code { get; }
	public string Description { get; }

	/// <summary>
	/// Optional <c>DiagnosticCatalog</c> quirk code (e.g. <c>quirk.event.bodyCast</c>) when
	/// this exception was thrown by an upstream-quirk-compat code path. Lets
	/// CLIs and editors annotate the error with "fixed in DB version X".
	/// </summary>
	public string? CompatCode { get; init; }

	/// <summary>
	/// Diagnostics that fired while processing the event that threw, including the throwing
	/// quirk itself. Empty unless a quirk was exercised. Gives a throwing quirk the same
	/// diagnostics channel as a non-throwing one, rather than surfacing only as
	/// <see cref="CompatCode"/>. Populated at the catch site, so it can be set after construction.
	/// </summary>
	public IReadOnlyList<Diagnostic> Diagnostics { get; set; } = Array.Empty<Diagnostic>();

	protected ProjectionException(string description, Exception? innerException = null)
		: base(description, innerException) {
		Description = description;
	}
}
