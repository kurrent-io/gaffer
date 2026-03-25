namespace Gaffer.Runtime.Events;

/// <summary>A single variable binding in a scope.</summary>
public sealed class DebugVariable {
	public required string Name { get; init; }
	public required string Value { get; init; }
	public required string Type { get; init; }

	/// <summary>Non-zero if the variable can be expanded. 0 for leaf values.</summary>
	public required int VariablesReference { get; init; }
}
