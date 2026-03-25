namespace Gaffer.Runtime.Events;

/// <summary>A single frame in the debug call stack. All positions are 1-based.</summary>
public sealed class DebugCallFrame {
	public required int Id { get; init; }
	public required string Name { get; init; }
	public required int Line { get; init; }
	public required int Column { get; init; }
}
