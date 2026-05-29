using Gaffer.Sdk.Diagnostics;

namespace Gaffer.Runtime.Events;

/// <summary>Result of feeding a single event to a projection session.</summary>
public sealed class FeedResult {
	/// <summary>Whether the event was processed or skipped.</summary>
	public required FeedStatus Status { get; init; }

	/// <summary>Partition key for the affected state. Null for skipped events and unpartitioned projections.</summary>
	public string? Partition { get; init; }

	/// <summary>Projection state for the affected partition after processing.</summary>
	public string? State { get; init; }

	/// <summary>Result for the affected partition. V1: transformed state (after <c>transformBy</c>/<c>filterBy</c>), or state if no transform; null if a filter excludes it. V2: post-handler state (transforms aren't invoked).</summary>
	public string? Result { get; init; }

	/// <summary>Shared state for biState projections, or null.</summary>
	public string? SharedState { get; init; }

	/// <summary>Events emitted during processing (emit/linkTo). Empty array when none.</summary>
	public EmittedEvent[] Emitted { get; init; } = [];

	/// <summary>Log messages from console.log calls. Empty array when none.</summary>
	public string[] Logs { get; init; } = [];

	/// <summary>Reason the event was skipped. Null when Status is Processed.</summary>
	public string? SkipReason { get; init; }

	/// <summary>Quirks encountered while processing this event (e.g. a biState string slot being JSON-quoted). Empty array when none. Distinct from compile-time <see cref="Gaffer.Sdk.ProjectionInfo"/> diagnostics: these are runtime, value-dependent, and have no source range.</summary>
	public Diagnostic[] Diagnostics { get; init; } = [];

	internal static FeedResult Skip(string reason) => new() { Status = FeedStatus.Skipped, SkipReason = reason };
}

/// <summary>Status of a feed operation.</summary>
public enum FeedStatus {
	/// <summary>Event was processed by the handler.</summary>
	Processed,
	/// <summary>Event was filtered out (V1 non-JSON, link, partitionBy null, etc).</summary>
	Skipped,
}
