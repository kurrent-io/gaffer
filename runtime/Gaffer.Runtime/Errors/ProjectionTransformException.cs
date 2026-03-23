namespace Gaffer.Runtime.Errors;

public sealed class ProjectionTransformException : GafferException {
	public override string Code => "projection-transform-error";
	public string? JsStack { get; }
	public int? Line { get; }
	public int? Column { get; }

	public ProjectionTransformException(
		string description,
		string? jsStack = null, int? line = null, int? column = null,
		Exception? innerException = null)
		: base(description, innerException) {
		JsStack = jsStack;
		Line = line;
		Column = column;
	}
}
