namespace Gaffer.Runtime.Errors;

public abstract class ProjectionException : Exception {
	public abstract string Code { get; }
	public string Description { get; }

	/// <summary>
	/// Optional <c>KnownBugs</c> code (e.g. <c>compat.event.bodyCast</c>) when
	/// this exception was thrown by an upstream-bug-compat code path. Lets
	/// CLIs and editors annotate the error with "fixed in DB version X".
	/// </summary>
	public string? CompatCode { get; set; }

	protected ProjectionException(string description, Exception? innerException = null)
		: base(description, innerException) {
		Description = description;
	}
}
