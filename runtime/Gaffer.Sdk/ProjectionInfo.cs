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
}
