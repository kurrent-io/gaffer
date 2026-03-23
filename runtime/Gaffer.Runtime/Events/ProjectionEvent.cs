namespace Gaffer.Runtime.Events;

/// <summary>An event to feed into a projection session.</summary>
public sealed class ProjectionEvent {
	/// <summary>The event type name (e.g. "OrderPlaced").</summary>
	public required string EventType { get; init; }

	/// <summary>The stream the event belongs to (e.g. "order-123").</summary>
	public required string StreamId { get; init; }

	/// <summary>Event payload as a JSON string, or null.</summary>
	public string? Data { get; init; }

	/// <summary>Event metadata as a JSON string, or null.</summary>
	public string? Metadata { get; init; }

	/// <summary>Link metadata (for resolved link events), or null.</summary>
	public string? LinkMetadata { get; init; }

	/// <summary>Event sequence number within its stream.</summary>
	public long SequenceNumber { get; init; }

	/// <summary>Whether the event data is JSON. Default: true.</summary>
	public bool IsJson { get; init; } = true;

	/// <summary>Unique event identifier.</summary>
	public Guid EventId { get; init; } = Guid.NewGuid();

	/// <summary>When the event was created.</summary>
	public DateTime Timestamp { get; init; } = DateTime.UtcNow;
}
