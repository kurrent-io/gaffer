namespace Gaffer.Runtime.Errors;

public abstract class ProjectionException : Exception {
	public abstract string Code { get; }
	public string Description { get; }

	protected ProjectionException(string description, Exception? innerException = null)
		: base(description, innerException) {
		Description = description;
	}
}
