namespace Gaffer.Runtime.Events;

/// <summary>Result of feeding a single event to a projection session.</summary>
public sealed class FeedResult {
	/// <summary>Whether the event was processed, skipped, or caused an error.</summary>
	public required FeedStatus Status { get; init; }

	/// <summary>Partition key for the affected state. Null for skipped events.</summary>
	public string? Partition { get; init; }

	/// <summary>Projection state for the affected partition after processing.</summary>
	public string? State { get; init; }

	/// <summary>Transformed result (after transformBy/filterBy), or state if no transform defined.</summary>
	public string? Result { get; init; }

	/// <summary>Shared state for biState projections, or null.</summary>
	public string? SharedState { get; init; }

	/// <summary>Events emitted during processing (emit/linkTo). Empty array when none.</summary>
	public EmittedEvent[] Emitted { get; init; } = [];

	/// <summary>Log messages from console.log calls. Empty array when none.</summary>
	public string[] Logs { get; init; } = [];

	internal static readonly FeedResult Skipped = new() { Status = FeedStatus.Skipped };
}

/// <summary>Status of a feed operation.</summary>
public enum FeedStatus {
	/// <summary>Event was processed by the handler.</summary>
	Processed,
	/// <summary>Event was filtered out (V1 non-JSON, link, partitionBy null, etc).</summary>
	Skipped,
	/// <summary>Processing failed. At the C API level, error details are in the JSON result.
	/// At the C# level, errors throw exceptions instead.</summary>
	Error,
}
