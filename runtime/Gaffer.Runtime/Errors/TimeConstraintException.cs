namespace Gaffer.Runtime.Errors;

internal sealed class TimeConstraintException : Exception {
	public bool IsCompilation { get; }
	public int ElapsedMs { get; }
	public int AllowedMs { get; }

	public TimeConstraintException(bool isCompilation, int elapsedMs, int allowedMs)
		: base($"Projection script took too long to {(isCompilation ? "compile" : "execute")} (took: {elapsedMs}ms, allowed: {allowedMs}ms)") {
		IsCompilation = isCompilation;
		ElapsedMs = elapsedMs;
		AllowedMs = allowedMs;
	}
}
