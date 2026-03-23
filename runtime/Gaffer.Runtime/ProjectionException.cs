namespace Gaffer.Runtime;

public sealed class ProjectionException : Exception {
	public ProjectionException(string message) : base(message) { }
	public ProjectionException(string message, Exception innerException) : base(message, innerException) { }
}
