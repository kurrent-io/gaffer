namespace Gaffer.Runtime.Errors;

public sealed class StateSerializationException : GafferException {
	public override string Code => "state-serialization-error";
	public string EventType { get; }
	public string StreamId { get; }
	public long SequenceNumber { get; }
	public string? Partition { get; }

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
