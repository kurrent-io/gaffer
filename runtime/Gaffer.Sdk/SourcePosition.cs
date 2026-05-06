namespace Gaffer.Sdk;

/// <summary>
/// 1-based line and column in projection source. Both 1-based for editor UI
/// consistency; consumers don't need to remember a per-axis offset. LSP clients
/// subtract 1 from each at the boundary.
/// </summary>
public sealed class SourcePosition {
	public required int Line { get; init; }
	public required int Column { get; init; }
}
