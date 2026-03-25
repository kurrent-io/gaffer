namespace Gaffer.Runtime.Events;

/// <summary>A single scope in a call frame's scope chain.</summary>
public sealed class DebugScopeInfo {
	public required string Name { get; init; }
	public required int VariablesReference { get; init; }
	public required bool Expensive { get; init; }
}
