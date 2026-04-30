using Gaffer.Sdk;

namespace Gaffer.Runtime.Projection;

/// <summary>
/// Maps the runtime's internal <see cref="QuerySources"/> (KurrentDB-shape) to
/// the public <see cref="ProjectionInfo"/> (gaffer-shape) returned to consumers.
/// </summary>
internal static class ProjectionInfoMapper {
	public static ProjectionInfo ToProjectionInfo(QuerySources s) => new() {
		AllStreams = s.AllStreams,
		AllEvents = s.AllEvents,
		Categories = s.Categories,
		Streams = s.Streams,
		Events = s.Events,
		ByStreams = s.ByStreams,
		ByCustomPartitions = s.ByCustomPartitions,
		BiState = s.IsBiState,
		DefinesHandlers = s.DefinesFold,
		DefinesStateTransform = s.DefinesStateTransform,
		ProducesResults = s.ProducesResults,
		HandlesDeletedNotifications = s.HandlesDeletedNotifications,
		IncludeLinks = s.IncludeLinks,
		ResultStreamName = s.ResultStreamName,
		PartitionResultStreamNamePattern = s.PartitionResultStreamNamePattern,
		ReorderEvents = s.ReorderEvents,
		ProcessingLag = s.ProcessingLag,
	};
}
