namespace Gaffer.Runtime.Events;

/// <summary>An event emitted by a projection via emit() or linkTo().</summary>
public sealed class EmittedEvent {
	/// <summary>Target stream for the emitted event.</summary>
	public required string StreamId { get; init; }

	/// <summary>Event type. "$>" for linkTo events, "$@" for linkStreamTo.</summary>
	public required string EventType { get; init; }

	/// <summary>Event data. For links, this is "{sequenceNumber}@{sourceStream}".</summary>
	public string? Data { get; init; }

	/// <summary>Whether the event data is JSON.</summary>
	public bool IsJson { get; init; }

	/// <summary>Unique event identifier.</summary>
	public Guid EventId { get; init; } = Guid.NewGuid();

	/// <summary>Optional metadata key-value pairs.</summary>
	public Dictionary<string, string?>? Metadata { get; init; }

	/// <summary>True if this is a link event (EventType is "$>").</summary>
	public bool IsLink => EventType == "$>";
}
