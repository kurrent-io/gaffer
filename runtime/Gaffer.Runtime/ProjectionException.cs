namespace Gaffer.Runtime;

/// <summary>Thrown when a projection handler encounters an error during event processing.</summary>
public sealed class ProjectionException : Exception {
	public ProjectionException(string message) : base(message) { }
	public ProjectionException(string message, Exception innerException) : base(message, innerException) { }
}
