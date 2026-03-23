namespace Gaffer.Runtime.Errors;

public abstract class GafferException : Exception {
	public abstract string Code { get; }
	public string Description { get; }

	protected GafferException(string description, Exception? innerException = null)
		: base(description, innerException) {
		Description = description;
	}
}
