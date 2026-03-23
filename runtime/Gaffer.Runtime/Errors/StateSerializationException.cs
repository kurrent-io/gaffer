namespace Gaffer.Runtime.Errors;

public sealed class StateSerializationException : ProjectionException {
	public override string Code => "state-serialization-error";
	public string EventType { get; }
	public string StreamId { get; }
	public long SequenceNumber { get; }
	public string? Partition { get; }

	public override string Message =>
		EventType.Length > 0
			? ErrorFormatter.FormatStateSerializationError(Description, EventType, StreamId, SequenceNumber, Partition)
			: base.Message;

	public StateSerializationException(
		string description, string eventType, string streamId,
		long sequenceNumber, string? partition = null,
		Exception? innerException = null)
		: base(description, innerException) {
		EventType = eventType;
		StreamId = streamId;
		SequenceNumber = sequenceNumber;
		Partition = partition;
	}
}
