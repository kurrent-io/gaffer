namespace Gaffer.Runtime.Errors;

public sealed class CompilationTimeoutException : GafferException {
	public override string Code => "compilation-timeout";
	public int ElapsedMs { get; }
	public int AllowedMs { get; }

	public CompilationTimeoutException(string description, int elapsedMs, int allowedMs, Exception? innerException = null)
		: base(description, innerException) {
		ElapsedMs = elapsedMs;
		AllowedMs = allowedMs;
	}
}
