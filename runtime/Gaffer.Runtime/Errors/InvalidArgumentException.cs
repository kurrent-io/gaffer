namespace Gaffer.Runtime.Errors;

public sealed class InvalidArgumentException : GafferException {
	public override string Code => "invalid-argument";
	public string Field { get; }

	public InvalidArgumentException(string description, string field, Exception? innerException = null)
		: base(description, innerException) {
		Field = field;
	}
}
