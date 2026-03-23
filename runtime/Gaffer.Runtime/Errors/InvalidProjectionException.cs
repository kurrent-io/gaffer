namespace Gaffer.Runtime.Errors;

public sealed class InvalidProjectionException : GafferException {
	public override string Code => "invalid-projection";
	public int? Index { get; }
	public int? Line { get; }
	public int? Column { get; }

	public InvalidProjectionException(string description, int index, int line, int column, Exception? innerException = null)
		: base(description, innerException) {
		Index = index;
		Line = line;
		Column = column;
	}

	public InvalidProjectionException(string description, Exception? innerException = null)
		: base(description, innerException) { }
}
