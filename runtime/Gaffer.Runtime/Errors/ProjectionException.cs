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

	protected ProjectionException(string description, Exception? innerException = null)
		: base(description, innerException) {
		Description = description;
	}
}
