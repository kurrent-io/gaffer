namespace Gaffer.Core.Events;

public sealed class EmittedEvent
{
    public required string StreamId { get; init; }
    public required string EventType { get; init; }
    public string? Data { get; init; }
    public bool IsJson { get; init; }
    public Guid EventId { get; init; } = Guid.NewGuid();
    public Dictionary<string, string?>? Metadata { get; init; }
    public bool IsLink => EventType == "$>";
}
