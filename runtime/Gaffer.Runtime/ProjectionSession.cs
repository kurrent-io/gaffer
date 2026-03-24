using System.Text.Json;
using Acornima;
using Gaffer.Runtime.Errors;
using Gaffer.Runtime.Events;
using Gaffer.Runtime.Projection;
using Jint;
using Jint.Runtime;

namespace Gaffer.Runtime;

/// <summary>
/// Runs a KurrentDB projection locally via Jint. Feed events, register callbacks,
/// query state. Not thread-safe - do not call from multiple threads concurrently.
/// </summary>
public sealed class ProjectionSession : IDisposable {
	private readonly JintProjectionHandler _handler;
	private readonly string _source;
	private readonly QuerySources _sources;
	private readonly Dictionary<string, string?> _stateCache = new();
	private readonly HashSet<string>? _handledEventTypes;
	private string? _sharedState;
	private readonly ProjectionVersion _version;
	private bool _sharedStateInitialized;
	private List<EmittedEvent> _pendingEmitted = new();
	private List<string> _pendingLogs = new();

	/// <summary>Called when the projection emits an event (emit or linkTo).</summary>
	public Action<EmittedEvent>? OnEmit { get; set; }

	/// <summary>Called when the projection calls console.log.</summary>
	public Action<string>? OnLog { get; set; }

	/// <summary>Called when projection state changes. Args: partition key, state JSON.</summary>
	public Action<string, string?>? OnStateChanged { get; set; }

	/// <summary>The projection's source definition (what streams/events it reads).</summary>
	public QuerySources Sources => _sources;

	/// <summary>
	/// Create a new projection session from JavaScript source code.
	/// Compiles and validates the projection immediately.
	/// </summary>
	/// <exception cref="InvalidProjectionException">Thrown if the JS source is invalid or the projection definition is wrong.</exception>
	/// <exception cref="CompilationTimeoutException">Thrown if compilation exceeds the timeout.</exception>
	public ProjectionSession(string source, ProjectionSessionOptions? options = null) {
		_source = source;
		var opts = options ?? new ProjectionSessionOptions();
		_version = opts.Version;

		try {
			_handler = new JintProjectionHandler(
				source,
				opts.EnableContentTypeValidation,
				opts.CompilationTimeout,
				opts.ExecutionTimeout,
				onLog: message => {
					_pendingLogs.Add(message);
					OnLog?.Invoke(message);
				},
				onEmit: emitted => {
					_pendingEmitted.Add(emitted);
					OnEmit?.Invoke(emitted);
				});
		} catch (ScriptPreparationException ex) when (ex.InnerException is ParseErrorException parseError) {
			throw new InvalidProjectionException(
				parseError.Description,
				parseError.LineNumber,
				parseError.Column,
				ex) { ProjectionSource = source };
		} catch (ScriptPreparationException ex) {
			throw new InvalidProjectionException(ex.InnerException?.Message ?? ex.Message, ex) { ProjectionSource = source };
		} catch (JavaScriptException ex) when (ex.Location.Start.Line > 0) {
			throw new InvalidProjectionException(
				ex.Message,
				ex.Location.Start.Line,
				ex.Location.Start.Column,
				ex) { ProjectionSource = source };
		} catch (JavaScriptException ex) {
			throw new InvalidProjectionException(ex.Message, ex) { ProjectionSource = source };
		} catch (TimeConstraintException ex) when (ex.IsCompilation) {
			throw new CompilationTimeoutException(
				$"Projection script took too long to compile ({ex.AllowedMs}ms limit)",
				ex.ElapsedMs, ex.AllowedMs, ex);
		} catch (ArgumentException ex) {
			throw new InvalidProjectionException(ex.Message, ex) { ProjectionSource = source };
		} catch (Exception ex) when (ex is not ProjectionException) {
			throw new InvalidProjectionException(ex.Message, ex) { ProjectionSource = source };
		}

		_sources = _handler.GetSourceDefinition();
		if (_sources.HandlesDeletedNotifications && !_sources.ByStreams)
			throw new InvalidProjectionException(
				"Deleted stream notifications are only supported with foreachStream()") { ProjectionSource = source };
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
	/// <exception cref="ProjectionHandlerException">Thrown if the JS handler throws an error.</exception>
	/// <exception cref="ExecutionTimeoutException">Thrown if the handler exceeds the timeout.</exception>
	/// <exception cref="MalformedEventException">Thrown if event data is malformed.</exception>
	/// <exception cref="StateSerializationException">Thrown if state contains unserializable values.</exception>
	public FeedResult Feed(ProjectionEvent @event) {
		_pendingEmitted.Clear();
		_pendingLogs.Clear();

		try {
			if (IsStreamDeletedEvent(@event, out var deletedStreamId))
				return FeedStreamDeleted(@event, deletedStreamId);
		} catch (JsonException ex) {
			throw new MalformedEventException(
				$"Failed to parse {StreamMetadataEventType} event data as JSON",
				@event.EventType, @event.StreamId, @event.SequenceNumber,
				innerException: ex);
		}

		if (!_sources.IncludeLinks &&
			(@event.EventType == "$>" || @event.LinkMetadata != null))
			return FeedResult.Skip("link");

		if (_version == ProjectionVersion.V1 && !@event.IsJson)
			return FeedResult.Skip("non-json");

		var partition = ResolvePartition(@event);
		if (partition == null)
			return FeedResult.Skip("no-partition");

		if (!ShouldProcess(@event))
			return FeedResult.Skip("unhandled");

		var isNewPartition = LoadPartitionState(partition);
		LoadSharedState();

		if (isNewPartition)
			_handler.ProcessPartitionCreated(partition, @event);

		try {
			var processed = _handler.ProcessEvent(
				partition,
				ResolveCategory(@event),
				@event,
				out var newState,
				out var newSharedState);

			if (processed)
				ProcessOutput(partition, newState, newSharedState);

			return BuildResult(partition);
		} catch (StateSerializationException ex) {
			var part = IsPartitioned ? partition : null;
			throw new StateSerializationException(
				ex.Description,
				@event.EventType, @event.StreamId, @event.SequenceNumber, part,
				ex.InnerException);
		} catch (Exception ex) when (ex is not ProjectionException) {
			throw WrapHandlerException(ex, @event, partition);
		}
	}

	private void ProcessOutput(string partition, string? newState, string? newSharedState) {
		_stateCache[partition] = newState;

		if (newState != null)
			OnStateChanged?.Invoke(partition, newState);

		if (_sources.IsBiState && newSharedState != null)
			_sharedState = newSharedState;
	}

	private FeedResult BuildResult(string partition) {
		var state = _stateCache.TryGetValue(partition, out var s) ? s : null;
		return new FeedResult {
			Status = FeedStatus.Processed,
			Partition = partition.Length > 0 ? partition : null,
			State = state,
			Result = TransformResult(),
			SharedState = _sources.IsBiState ? _sharedState : null,
			Emitted = _pendingEmitted.ToArray(),
			Logs = _pendingLogs.ToArray(),
		};
	}

	/// <summary>Get current state for a partition, or null if the partition has not been seen.</summary>
	/// <param name="partition">Partition key, or null for the default (unpartitioned) state.</param>
	public string? GetState(string? partition = null) =>
		_stateCache.TryGetValue(partition ?? "", out var state) ? state : null;

	/// <summary>Get shared state for biState projections, or null.</summary>
	public string? GetSharedState() => _sharedState;

	/// <summary>Restore state for a partition (e.g. from a cache).</summary>
	/// <exception cref="InvalidArgumentException">Thrown if stateJson is null or empty.</exception>
	public void SetState(string? partition, string stateJson) {
		if (string.IsNullOrEmpty(stateJson))
			throw new InvalidArgumentException("State JSON must not be null or empty", "stateJson");
		_stateCache[partition ?? ""] = stateJson;
	}

	/// <summary>
	/// Get the transformed result for a partition (applies transformBy/filterBy).
	/// Returns null for unknown partitions or filtered results.
	/// </summary>
	/// <exception cref="ProjectionTransformException">Thrown if transformBy/filterBy throws.</exception>
	public string? GetResult(string? partition = null) {
		var key = partition ?? "";
		if (!_stateCache.ContainsKey(key))
			return null;
		LoadPartitionState(key);
		if (_sharedStateInitialized)
			LoadSharedState();
		return TransformResult();
	}

	private string? TransformResult() {
		try {
			return _handler.TransformStateToResult();
		} catch (JavaScriptException ex) {
			int? line = ex.Location.Start.Line > 0 ? ex.Location.Start.Line : null;
			int? column = line != null ? ex.Location.Start.Column : null;
			throw new ProjectionTransformException(
				ex.Message,
				ex.JavaScriptStackTrace, line, column,
				ex) { ProjectionSource = _source };
		} catch (TimeConstraintException ex) {
			throw new ProjectionTransformException(
				$"Projection transform took too long to execute ({ex.AllowedMs}ms limit)",
				innerException: ex) { ProjectionSource = _source };
		} catch (StateSerializationException ex) {
			throw new ProjectionTransformException(
				ex.Description,
				innerException: ex) { ProjectionSource = _source };
		} catch (Exception ex) when (ex is not ProjectionException) {
			throw new ProjectionTransformException(ex.Message, innerException: ex) { ProjectionSource = _source };
		}
	}

	private bool IsPartitioned => _sources.ByStreams || _sources.ByCustomPartitions;

	private ProjectionException WrapHandlerException(Exception ex, ProjectionEvent @event, string partition) {
		var part = IsPartitioned ? partition : null;
		return ex switch {
			TimeConstraintException tc => new ExecutionTimeoutException(
				$"Projection script took too long to execute ({tc.AllowedMs}ms limit)",
				tc.ElapsedMs, tc.AllowedMs,
				@event.EventType, @event.StreamId, @event.SequenceNumber, part,
				tc),
			MalformedEventDataException med => new MalformedEventException(
				med.Message,
				@event.EventType, @event.StreamId, @event.SequenceNumber, part,
				med.InnerException),
			JavaScriptException js => new ProjectionHandlerException(
				js.Message,
				@event.EventType, @event.StreamId, @event.SequenceNumber, part,
				js.JavaScriptStackTrace,
				js.Location.Start.Line > 0 ? js.Location.Start.Line : null,
				js.Location.Start.Line > 0 ? js.Location.Start.Column : null,
				js) { ProjectionSource = _source },
			_ => new ProjectionHandlerException(
				ex.Message,
				@event.EventType, @event.StreamId, @event.SequenceNumber, part,
				innerException: ex) { ProjectionSource = _source },
		};
	}

	/// <summary>Get the partition key that would be computed for an event.</summary>
	public string? GetPartitionKey(ProjectionEvent @event) => ResolvePartition(@event);

	private string? ResolvePartition(ProjectionEvent @event) {
		if (_sources.ByCustomPartitions)
			return _handler.GetStatePartition(@event, ResolveCategory(@event));

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

	private FeedResult FeedStreamDeleted(ProjectionEvent @event, string partition) {
		if (!_sources.HandlesDeletedNotifications)
			return FeedResult.Skip("no-delete-handler");

		LoadPartitionState(partition);
		LoadSharedState();

		try {
			_handler.ProcessPartitionDeleted(partition, out var newState);

			_stateCache[partition] = newState;

			if (newState != null)
				OnStateChanged?.Invoke(partition, newState);

			return BuildResult(partition);
		} catch (StateSerializationException ex) {
			var part = IsPartitioned ? partition : null;
			throw new StateSerializationException(
				ex.Description,
				@event.EventType, @event.StreamId, @event.SequenceNumber, part,
				ex.InnerException);
		} catch (Exception ex) when (ex is not ProjectionException) {
			throw WrapHandlerException(ex, @event, partition);
		}
	}

	/// <summary>
	/// Loads partition state from cache or initializes fresh.
	/// Returns true if the partition is new (not previously seen).
	/// </summary>
	private bool LoadPartitionState(string partition) {
		if (_stateCache.TryGetValue(partition, out var cached)) {
			if (cached != null) {
				_handler.Load(cached);
			} else if (_version == ProjectionVersion.V1) {
				_handler.Initialize();
			} else {
				_handler.Load(null);
			}
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

/// <summary>KurrentDB projection engine version.</summary>
public enum ProjectionVersion {
	/// <summary>V2 engine (default). Passes all events to the handler.</summary>
	V2 = 0,
	/// <summary>V1 engine. Drops non-JSON events before they reach the handler.</summary>
	V1 = 1,
}

/// <summary>Configuration options for a projection session.</summary>
public sealed class ProjectionSessionOptions {
	/// <summary>Projection engine version. Default: V2.</summary>
	public ProjectionVersion Version { get; init; } = ProjectionVersion.V2;

	/// <summary>Maximum time for JS compilation. Default: 5 seconds.</summary>
	public TimeSpan CompilationTimeout { get; init; } = TimeSpan.FromSeconds(5);

	/// <summary>Maximum time for JS handler execution per event. Default: 5 seconds.</summary>
	public TimeSpan ExecutionTimeout { get; init; } = TimeSpan.FromSeconds(5);

	/// <summary>Enable Jint debug hooks for breakpoints and stepping. Has performance overhead.</summary>
	public bool Debug { get; init; }

	/// <summary>
	/// When true, non-JSON events with empty data are still passed to handlers.
	/// When false (default), they are skipped. Only meaningful in V2 mode;
	/// V1 drops non-JSON events at the subscription level before they reach the handler.
	/// </summary>
	public bool EnableContentTypeValidation { get; init; }
}
