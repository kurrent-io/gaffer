namespace Gaffer.Runtime.Errors;

public sealed class ProjectionHandlerException : ProjectionException {
	public override string Code => "handler-error";
	public string? JsStack { get; }
	public int? Line { get; }
	public int? Column { get; }
	public string EventType { get; }
	public string StreamId { get; }
	public long SequenceNumber { get; }
	public string? Partition { get; }
	public string? ProjectionSource { get; init; }

	public override string Message => ProjectionSource != null
		? ErrorFormatter.FormatHandlerError(
			Description, ProjectionSource, EventType, StreamId,
			SequenceNumber, Partition, JsStack, Line, Column)
		: base.Message;

	public ProjectionHandlerException(
		string description, string eventType, string streamId,
		long sequenceNumber, string? partition = null,
		string? jsStack = null, int? line = null, int? column = null,
		Exception? innerException = null)
		: base(description, innerException) {
		JsStack = jsStack;
		Line = line;
		Column = column;
		EventType = eventType;
		StreamId = streamId;
		SequenceNumber = sequenceNumber;
		Partition = partition;
	}
}
