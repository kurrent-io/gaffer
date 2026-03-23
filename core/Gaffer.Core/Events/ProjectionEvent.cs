namespace Gaffer.Core.Events;

public sealed class ProjectionEvent
{
    public required string EventType { get; init; }
    public required string StreamId { get; init; }
    public string? Data { get; init; }
    public string? Metadata { get; init; }
    public long SequenceNumber { get; init; }
    public bool IsJson { get; init; } = true;
    public Guid EventId { get; init; } = Guid.NewGuid();
    public DateTime Timestamp { get; init; } = DateTime.UtcNow;
}
