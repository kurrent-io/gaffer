using Gaffer.Sdk.Diagnostics;

namespace Gaffer.Sdk;

/// <summary>
/// Describes a projection's source configuration and features.
/// </summary>
public sealed class ProjectionInfo {
	public bool AllStreams { get; init; }
	public bool AllEvents { get; init; }
	public string[]? Categories { get; init; }
	public string[]? Streams { get; init; }
	public string[]? Events { get; init; }
	public bool ByStreams { get; init; }
	public bool ByCustomPartitions { get; init; }
	public bool BiState { get; init; }
	public bool DefinesHandlers { get; init; }
	public bool DefinesStateTransform { get; init; }
	public bool ProducesResults { get; init; }
	public bool HandlesDeletedNotifications { get; init; }
	public bool IncludeLinks { get; init; }
	public string? ResultStreamName { get; init; }
	public string? PartitionResultStreamNamePattern { get; init; }
	public bool ReorderEvents { get; init; }
	public int? ProcessingLag { get; init; }
	public Diagnostic[]? Diagnostics { get; init; }

	/// <summary>
	/// Structural snapshot of the projection's source. Populated when
	/// the FFI caller passes <c>IncludeShape:true</c>; <c>null</c>
	/// otherwise. Walking is gated by the flag because LSP and most
	/// other consumers don't need the data and shouldn't pay the
	/// extra AST pass.
	/// </summary>
	public ProjectionShape? Shape { get; init; }
}
