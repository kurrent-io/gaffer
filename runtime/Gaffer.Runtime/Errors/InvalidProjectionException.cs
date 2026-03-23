namespace Gaffer.Runtime.Errors;

public sealed class InvalidProjectionException : GafferException {
	public override string Code => "invalid-projection";
	public int? Line { get; }
	public int? Column { get; }
	public string? ProjectionSource { get; init; }

	public override string Message => ProjectionSource != null
		? ErrorFormatter.FormatInvalidProjection(Description, ProjectionSource, Line, Column)
		: base.Message;

	public InvalidProjectionException(string description, int line, int column, Exception? innerException = null)
		: base(description, innerException) {
		Line = line;
		Column = column;
	}

	public InvalidProjectionException(string description, Exception? innerException = null)
		: base(description, innerException) { }
}
