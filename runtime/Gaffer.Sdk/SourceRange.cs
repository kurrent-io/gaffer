namespace Gaffer.Sdk;

/// <summary>Inclusive start, exclusive end - matches LSP and most editor APIs.</summary>
public sealed class SourceRange {
	public required SourcePosition Start { get; init; }
	public required SourcePosition End { get; init; }
}
