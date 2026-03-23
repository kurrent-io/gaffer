using System.Diagnostics;
using System.Text.Json;
using Gaffer.Runtime.Events;
using Gaffer.Runtime.Projection;

namespace Gaffer.Runtime;

/// <summary>
/// Runs a KurrentDB projection locally via Jint. Feed events, register callbacks,
/// query state. Not thread-safe - do not call from multiple threads concurrently.
/// </summary>
public sealed class ProjectionSession : IDisposable {
	private readonly JintProjectionHandler _handler;
	private readonly QuerySources _sources;
	private readonly Dictionary<string, string?> _stateCache = new();
	private readonly HashSet<string>? _handledEventTypes;
	private string? _sharedState;
	private readonly TimeSpan _handlerTimeout;
	private readonly Stopwatch _handlerStopwatch = new();
	private bool _sharedStateInitialized;

	/// <summary>Called when the projection emits an event (emit or linkTo).</summary>
	public Action<EmittedEvent>? OnEmit { get; set; }

	/// <summary>Called when the projection calls console.log.</summary>
	public Action<string>? OnLog { get; set; }

	/// <summary>Called when projection state changes. Args: partition key, state JSON.</summary>
	public Action<string, string?>? OnStateChanged { get; set; }

	/// <summary>Called when a handler exceeds the timeout. Args: event type, duration ms.</summary>
	public Action<string, int>? OnSlowHandler { get; set; }

	/// <summary>The projection's source definition (what streams/events it reads).</summary>
	public QuerySources Sources => _sources;

	/// <summary>
	/// Create a new projection session from JavaScript source code.
	/// Compiles and validates the projection immediately.
	/// </summary>
	/// <exception cref="Exception">Thrown if the JS source is invalid or fails to compile.</exception>
	public ProjectionSession(string source, ProjectionSessionOptions? options = null) {
		var opts = options ?? new ProjectionSessionOptions();
		_handlerTimeout = TimeSpan.FromMilliseconds(opts.HandlerTimeoutMs);

		_handler = new JintProjectionHandler(
			source,
			opts.EnableContentTypeValidation,
			opts.CompilationTimeout,
			opts.ExecutionTimeout,
			message => OnLog?.Invoke(message));

		_sources = _handler.GetSourceDefinition();
		if (!_sources.AllEvents && _sources.Events != null)
			_handledEventTypes = new HashSet<string>(_sources.Events, StringComparer.Ordinal);
	}

	public void Dispose() => _handler.Dispose();

	/// <summary>Event type for hard-deleted streams.</summary>
	public const string StreamDeletedEventType = "$streamDeleted";

	/// <summary>Event type for stream metadata (used in soft deletes).</summary>
	public const string StreamMetadataEventType = "$metadata";
	private const string MetastreamPrefix = "$$";

	/// <summary>
	/// Feed a single event to the projection. Blocks until processing completes.
	/// Automatically detects hard deletes ($streamDeleted) and soft deletes
	/// ($metadata on $$stream with $tb=long.MaxValue) and routes to the
	/// $deleted handler if defined.
	/// </summary>
	/// <exception cref="ProjectionException">Thrown if the JS handler throws an error.</exception>
	public void Feed(ProjectionEvent @event) {
		if (IsStreamDeletedEvent(@event, out var deletedStreamId)) {
			FeedStreamDeleted(deletedStreamId);
			return;
		}

		var partition = ResolvePartition(@event);

		if (!ShouldProcess(@event))
			return;

		var isNewPartition = LoadPartitionState(partition);
		LoadSharedState();

		if (isNewPartition) {
			_handler.ProcessPartitionCreated(partition, @event, out var createdEmitted);
			if (createdEmitted != null)
				foreach (var e in createdEmitted)
					OnEmit?.Invoke(e);
		}

		_handlerStopwatch.Restart();
		try {
			var processed = _handler.ProcessEvent(
				partition,
				ResolveCategory(@event),
				@event,
				out var newState,
				out var newSharedState,
				out var emittedEvents);
			_handlerStopwatch.Stop();

			if (processed)
				ProcessOutput(partition, @event, newState, newSharedState, emittedEvents);
		} catch (Exception ex) when (ex is not ProjectionException) {
			_handlerStopwatch.Stop();
			throw new ProjectionException(
				$"Error processing {@event.EventType} in partition '{partition}': {ex.Message}", ex);
		}
	}

	private void ProcessOutput(string partition, ProjectionEvent @event,
		string? newState, string? newSharedState, EmittedEvent[]? emittedEvents) {

		if (_handlerStopwatch.Elapsed > _handlerTimeout)
			OnSlowHandler?.Invoke(@event.EventType, (int)_handlerStopwatch.ElapsedMilliseconds);

		_stateCache[partition] = newState;

		if (newState != null)
			OnStateChanged?.Invoke(partition, newState);

		if (_sources.IsBiState && newSharedState != null)
			_sharedState = newSharedState;

		if (emittedEvents != null)
			foreach (var emitted in emittedEvents)
				OnEmit?.Invoke(emitted);
	}

	/// <summary>Get current state for a partition, or null if the partition has not been seen.</summary>
	/// <param name="partition">Partition key, or null for the default (unpartitioned) state.</param>
	public string? GetState(string? partition = null) =>
		_stateCache.TryGetValue(partition ?? "", out var state) ? state : null;

	/// <summary>Get shared state for biState projections, or null.</summary>
	public string? GetSharedState() => _sharedState;

	/// <summary>Restore state for a partition (e.g. from a cache).</summary>
	public void SetState(string? partition, string stateJson) {
		_stateCache[partition ?? ""] = stateJson;
	}

	/// <summary>
	/// Get the transformed result for a partition (applies transformBy/filterBy).
	/// Returns null for unknown partitions or filtered results.
	/// </summary>
	public string? GetResult(string? partition = null) {
		var key = partition ?? "";
		if (!_stateCache.ContainsKey(key))
			return null;
		LoadPartitionState(key);
		if (_sharedStateInitialized)
			LoadSharedState();
		return _handler.TransformStateToResult();
	}

	/// <summary>Get the partition key that would be computed for an event.</summary>
	public string? GetPartitionKey(ProjectionEvent @event) => ResolvePartition(@event);

	private string ResolvePartition(ProjectionEvent @event) {
		if (_sources.ByCustomPartitions)
			return _handler.GetStatePartition(@event, ResolveCategory(@event)) ?? "";

		if (_sources.ByStreams)
			return @event.StreamId;

		return "";
	}

	private static string ResolveCategory(ProjectionEvent @event) {
		var streamId = @event.StreamId;
		var dashIndex = streamId.IndexOf('-');
		return dashIndex >= 0 ? streamId[..dashIndex] : streamId;
	}

	private bool ShouldProcess(ProjectionEvent @event) =>
		_handledEventTypes == null || _handledEventTypes.Contains(@event.EventType);

	private static bool IsStreamDeletedEvent(ProjectionEvent @event, out string deletedStreamId) {
		if (@event.EventType == StreamDeletedEventType) {
			deletedStreamId = @event.StreamId;
			return true;
		}

		if (@event.EventType == StreamMetadataEventType &&
			@event.StreamId.StartsWith(MetastreamPrefix) &&
			@event.Data != null) {
			// Matches KurrentDB: throws on malformed metadata rather than ignoring
			using var doc = JsonDocument.Parse(@event.Data);
			if (doc.RootElement.TryGetProperty("$tb", out var tb) &&
				tb.ValueKind == JsonValueKind.Number &&
				tb.GetInt64() == long.MaxValue) {
				deletedStreamId = @event.StreamId[MetastreamPrefix.Length..];
				return true;
			}
		}

		deletedStreamId = "";
		return false;
	}

	private void FeedStreamDeleted(string partition) {
		if (!_sources.HandlesDeletedNotifications)
			return;

		LoadPartitionState(partition);
		LoadSharedState();

		_handler.ProcessPartitionDeleted(partition, out var newState);

		_stateCache[partition] = newState;

		if (newState != null)
			OnStateChanged?.Invoke(partition, newState);
	}

	/// <summary>
	/// Loads partition state from cache or initializes fresh.
	/// Returns true if the partition is new (not previously seen).
	/// </summary>
	private bool LoadPartitionState(string partition) {
		if (_stateCache.ContainsKey(partition)) {
			var cached = _stateCache[partition];
			if (cached != null)
				_handler.Load(cached);
			else
				_handler.Load(null);
			return false;
		}

		_handler.Initialize();
		return true;
	}

	/// <summary>
	/// Loads shared state for biState projections.
	/// InitializeShared called once, LoadShared for subsequent events.
	/// </summary>
	private void LoadSharedState() {
		if (!_sources.IsBiState)
			return;

		if (!_sharedStateInitialized) {
			_handler.InitializeShared();
			_sharedStateInitialized = true;
		} else if (_sharedState != null) {
			_handler.LoadShared(_sharedState);
		}
	}
}

/// <summary>Configuration options for a projection session.</summary>
public sealed class ProjectionSessionOptions {
	/// <summary>Handler timeout in milliseconds. Triggers OnSlowHandler callback. Default: 250.</summary>
	public int HandlerTimeoutMs { get; init; } = 250;

	/// <summary>Maximum time for JS compilation. Default: 5 seconds.</summary>
	public TimeSpan CompilationTimeout { get; init; } = TimeSpan.FromSeconds(5);

	/// <summary>Maximum time for JS handler execution per event. Default: 5 seconds.</summary>
	public TimeSpan ExecutionTimeout { get; init; } = TimeSpan.FromSeconds(5);

	/// <summary>Enable Jint debug hooks for breakpoints and stepping. Has performance overhead.</summary>
	public bool Debug { get; init; }

	/// <summary>
	/// When true, non-JSON events with empty data are still passed to handlers (V1 compat).
	/// When false (default), they are skipped.
	/// </summary>
	public bool EnableContentTypeValidation { get; init; }
}
