namespace Gaffer.Runtime.Events;

/// <summary>
/// Information about a debug pause (breakpoint hit, debugger statement, or step completed).
/// All positions are 1-based.
/// </summary>
public sealed class BreakInfo {
	/// <summary>Why execution paused: "breakpoint", "debugger_statement", or "step".</summary>
	public required string Reason { get; init; }

	/// <summary>Line number where execution paused (1-based).</summary>
	public required int Line { get; init; }

	/// <summary>Column number where execution paused (1-based).</summary>
	public required int Column { get; init; }
}
