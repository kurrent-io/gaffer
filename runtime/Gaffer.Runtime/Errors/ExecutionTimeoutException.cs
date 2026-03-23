namespace Gaffer.Runtime.Errors;

public sealed class ExecutionTimeoutException : ProjectionException {
	public override string Code => "execution-timeout";
	public int ElapsedMs { get; }
	public int AllowedMs { get; }
	public string EventType { get; }
	public string StreamId { get; }
	public long SequenceNumber { get; }
	public string? Partition { get; }

	public override string Message =>
		ErrorFormatter.FormatWithEventContext(Description, EventType, StreamId, SequenceNumber, Partition);

	public ExecutionTimeoutException(
		string description, int elapsedMs, int allowedMs,
		string eventType, string streamId, long sequenceNumber, string? partition = null,
		Exception? innerException = null)
		: base(description, innerException) {
		ElapsedMs = elapsedMs;
		AllowedMs = allowedMs;
		EventType = eventType;
		StreamId = streamId;
		SequenceNumber = sequenceNumber;
		Partition = partition;
	}
}
