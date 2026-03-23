namespace Gaffer.Runtime.Errors;

public sealed class ProjectionTransformException : GafferException {
	public override string Code => "projection-transform-error";
	public string? JsStack { get; }
	public int? Line { get; }
	public int? Column { get; }
	public string? ProjectionSource { get; init; }

	public override string Message => ProjectionSource != null
		? ErrorFormatter.FormatTransformError(Description, ProjectionSource, JsStack, Line, Column)
		: base.Message;

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
