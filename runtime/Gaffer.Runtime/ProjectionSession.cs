using System.Text.Json;
using Acornima;
using Gaffer.Runtime.Errors;
using Gaffer.Runtime.Events;
using Gaffer.Runtime.Projection;
using Gaffer.Sdk.Diagnostics;
using Gaffer.Sdk.Versioning;
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
	private readonly Diagnostic[]? _diagnostics;
	private readonly Gaffer.Sdk.ProjectionShape? _shape;
	private readonly Dictionary<string, string?> _stateCache = new();
	private readonly HashSet<string>? _handledEventTypes;
	private string? _sharedState;
	private readonly ProjectionVersion _version;
	private readonly KurrentDbVersion? _quirksVersion;
	private bool _sharedStateInitialized;
	private List<EmittedEvent> _pendingEmitted = new();
	private List<string> _pendingLogs = new();
	private readonly List<Diagnostic> _pendingDiagnostics = new();

	/// <summary>Called when the projection emits an event (emit or linkTo).</summary>
	public Action<EmittedEvent>? OnEmit { get; set; }

	/// <summary>Called when the projection calls console.log.</summary>
	public Action<string>? OnLog { get; set; }

	/// <summary>Called when a quirk fires while processing an event (e.g. a biState string slot being JSON-quoted), at the point it fires. Also recorded on <see cref="Events.FeedResult.Diagnostics"/>.</summary>
	public Action<Diagnostic>? OnDiagnostic { get; set; }

	/// <summary>Called when projection state changes. Args: partition key, state JSON.</summary>
	public Action<string, string?>? OnStateChanged { get; set; }

	/// <summary>Called when execution pauses at a breakpoint or debugger statement. Informational only.</summary>
	public Action<BreakInfo>? OnBreak { get; set; }

	/// <summary>The projection's source definition (what streams/events it reads).</summary>
	public QuerySources Sources => _sources;

	/// <summary>Compile-time diagnostics, or null if there are none.</summary>
	public Diagnostic[]? Diagnostics => _diagnostics;

	/// <summary>
	/// Structural shape snapshot of the projection source. Populated
	/// only when <see cref="ProjectionSessionOptions.IncludeShape"/>
	/// was set at construction; null otherwise. Returned to the FFI
	/// caller via <see cref="Gaffer.Sdk.ProjectionInfo.Shape"/>.
	/// </summary>
	public Gaffer.Sdk.ProjectionShape? Shape => _shape;

	/// <summary>
	/// Create a new projection session from JavaScript source code.
	/// Compiles and validates the projection immediately.
	/// </summary>
	/// <exception cref="InvalidProjectionException">Thrown if the JS source is invalid or the projection definition is wrong.</exception>
	/// <exception cref="CompilationTimeoutException">Thrown if compilation exceeds the timeout.</exception>
	public ProjectionSession(string source, ProjectionSessionOptions options) {
		ArgumentNullException.ThrowIfNull(options);
		_source = source;
		var opts = options;
		_version = opts.EngineVersion;
		_quirksVersion = opts.QuirksVersion;

		// V2 engine doesn't exist in DB versions before its introduction.
		// Reject up-front so the user gets a clear error instead of mysterious
		// downstream failures. Unversioned (null QuirksVersion) is permissive -
		// matches the unversioned-defaults model.
		if (_version == ProjectionVersion.V2 && !KnownFeatures.ProjectionsV2.AvailableAt(_quirksVersion)) {
			// Field name matches the JSON option key the caller provided,
			// not the C# property - that's what bindings expose to users.
			throw new InvalidArgumentException(
				$"V2 engine requires KurrentDB {KnownFeatures.ProjectionsV2.IntroducedIn} or later; got {_quirksVersion}.",
				"quirksVersion");
		}

		try {
			_handler = new JintProjectionHandler(
				source,
				opts.CompilationTimeout,
				opts.ExecutionTimeout,
				_version,
				onLog: message => {
					_pendingLogs.Add(message);
					OnLog?.Invoke(message);
				},
				onEmit: emitted => {
					_pendingEmitted.Add(emitted);
					OnEmit?.Invoke(emitted);
				},
				// Record for the FeedResult batch and stream live at the point
				// of firing. No dedup needed: the V2 result-pass through
				// PrepareOutput is told not to re-report (TransformStateToResult),
				// so each genuine occurrence fires once.
				onDiagnostic: diagnostic => {
					_pendingDiagnostics.Add(diagnostic);
					OnDiagnostic?.Invoke(diagnostic);
				},
				debug: opts.Debug,
				quirksVersion: _quirksVersion);

			_handler.OnBreak = info => OnBreak?.Invoke(info);
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

		// Anything that throws past this point would leak _handler (and its
		// Jint Engine + BlockingCollection); dispose it on failure.
		try {
			_sources = _handler.GetSourceDefinition();
			if (_sources.HandlesDeletedNotifications && !_sources.ByStreams)
				throw new InvalidProjectionException(
					"Deleted stream notifications are only supported with foreachStream()") { ProjectionSource = source };
			if (!_sources.AllEvents && _sources.Events != null)
				_handledEventTypes = new HashSet<string>(_sources.Events, StringComparer.Ordinal);

			// Combined scan: one parse, optional shape walk piggy-
			// backs on the diagnostic pass. IncludeShape gates the
			// shape walker without affecting diagnostic collection.
			(_diagnostics, _shape) = DiagnosticCollector.ScanWithShape(
				source, _quirksVersion, _version, opts.IncludeShape);
		} catch {
			_handler.Dispose();
			throw;
		}
	}

	public void Dispose() {
		if (_handler.IsPaused) {
			_handler.ClearBreakpoints();
			try { _handler.Continue(); } catch { /* best effort */ }
		}
		_handler.Dispose();
	}

	/// <summary>
	/// Set a breakpoint, snapping to the nearest breakable position on or after the given position.
	/// Returns the actual (line, column) where it was set (1-based), or null if no breakable position found.
	/// </summary>
	public (int Line, int Column)? SetBreakpoint(int line, int column = 1, string? condition = null, string? hitCondition = null, string? logMessage = null) =>
		_handler.SetBreakpoint(line, column, condition, hitCondition, logMessage);

	/// <summary>Remove all breakpoints.</summary>
	public void ClearBreakpoints() => _handler.ClearBreakpoints();

	/// <summary>Resume execution after a debug pause.</summary>
	public void Continue() => _handler.Continue();

	/// <summary>Step into the next function call. Only valid while paused.</summary>
	public void StepInto() => _handler.StepInto();

	/// <summary>Step over the next statement. Only valid while paused.</summary>
	public void StepOver() => _handler.StepOver();

	/// <summary>Step out of the current function. Only valid while paused.</summary>
	public void StepOut() => _handler.StepOut();

	/// <summary>Whether the session is currently paused at a breakpoint.</summary>
	public bool IsPaused => _handler.IsPaused;

	/// <summary>Request a pause before the next event is processed.</summary>
	public void Pause() => _handler.Pause();

	/// <summary>Get the call stack. Only valid while paused.</summary>
	public DebugCallFrame[] GetCallStack() => _handler.GetCallStack();

	/// <summary>Get scopes for a call frame. Only valid while paused.</summary>
	public DebugScopeInfo[] GetScopes(int frameIndex) => _handler.GetScopes(frameIndex);

	/// <summary>Get variables for a scope or object reference. Only valid while paused.</summary>
	public DebugVariable[] GetVariables(int variablesReference) => _handler.GetVariables(variablesReference);

	/// <summary>Evaluate an expression in the current debug context. Only valid while paused.</summary>
	public DebugVariable Evaluate(string expression) => _handler.Evaluate(expression);

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
		_handler.HandlePauseIfRequested();
		_pendingEmitted.Clear();
		_pendingLogs.Clear();
		_pendingDiagnostics.Clear();

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
		} catch (OperationCanceledException) {
			throw;
		} catch (StateSerializationException ex) {
			var part = IsPartitioned ? partition : null;
			throw new StateSerializationException(
				ex.Description,
				@event.EventType, @event.StreamId, @event.SequenceNumber, part,
				ex.InnerException) { CompatCode = ex.CompatCode };
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
			Diagnostics = _pendingDiagnostics.ToArray(),
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
	/// Get the result for a partition. Under V1, applies any registered
	/// <c>transformBy</c>/<c>filterBy</c> functions and returns the transformed
	/// state (or null if a filter excludes it). Under V2, returns the
	/// post-handler state directly - V2 doesn't iterate transforms. Returns
	/// null for unknown partitions.
	/// </summary>
	/// <exception cref="ProjectionTransformException">V1 only - thrown if a registered <c>transformBy</c>/<c>filterBy</c> function throws. V2 doesn't invoke transforms, so this never fires under V2.</exception>
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
				ex) { ProjectionSource = _source, CompatCode = ExtractCompatCode(ex) };
		} catch (TimeConstraintException ex) {
			throw new ProjectionTransformException(
				$"Projection transform took too long to execute ({ex.AllowedMs}ms limit)",
				innerException: ex) { ProjectionSource = _source };
		} catch (StateSerializationException ex) {
			throw new ProjectionTransformException(
				ex.Description,
				innerException: ex) { ProjectionSource = _source, CompatCode = ex.CompatCode };
		} catch (Exception ex) when (ex is not ProjectionException) {
			throw new ProjectionTransformException(ex.Message, innerException: ex) { ProjectionSource = _source, CompatCode = ExtractCompatCode(ex) };
		}
	}

	private bool IsPartitioned => _sources.ByStreams || _sources.ByCustomPartitions;

	private ProjectionException WrapHandlerException(Exception ex, ProjectionEvent @event, string partition) {
		var part = IsPartitioned ? partition : null;
		var compatCode = ExtractCompatCode(ex);
		return ex switch {
			TimeConstraintException tc => new ExecutionTimeoutException(
				$"Projection script took too long to execute ({tc.AllowedMs}ms limit)",
				tc.ElapsedMs, tc.AllowedMs,
				@event.EventType, @event.StreamId, @event.SequenceNumber, part,
				tc) { CompatCode = compatCode },
			MalformedEventDataException med => new MalformedEventException(
				med.Message,
				@event.EventType, @event.StreamId, @event.SequenceNumber, part,
				med.InnerException) { CompatCode = compatCode },
			JavaScriptException js => new ProjectionHandlerException(
				js.Message,
				@event.EventType, @event.StreamId, @event.SequenceNumber, part,
				js.JavaScriptStackTrace,
				js.Location.Start.Line > 0 ? js.Location.Start.Line : null,
				js.Location.Start.Line > 0 ? js.Location.Start.Column : null,
				js) { ProjectionSource = _source, CompatCode = compatCode },
			_ => new ProjectionHandlerException(
				ex.Message,
				@event.EventType, @event.StreamId, @event.SequenceNumber, part,
				innerException: ex) { ProjectionSource = _source, CompatCode = compatCode },
		};
	}

	/// <summary>
	/// Walk the exception chain looking for a <c>GafferCompatCode</c> stashed
	/// on <see cref="Exception.Data"/> by a quirk-firing branch in the handler.
	/// </summary>
	private static string? ExtractCompatCode(Exception? ex) {
		for (var cur = ex; cur != null; cur = cur.InnerException) {
			if (cur.Data[JintProjectionHandler.CompatCodeDataKey] is string code)
				return code;
		}
		return null;
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
				ex.InnerException) { CompatCode = ex.CompatCode };
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
	/// <summary>V1 engine. Drops non-JSON events before they reach the handler.</summary>
	V1 = 1,
	/// <summary>V2 engine. Passes all events to the handler.</summary>
	V2 = 2,
}

/// <summary>Configuration options for a projection session. EngineVersion is required.</summary>
public sealed class ProjectionSessionOptions {
	/// <summary>Projection engine version. Required.</summary>
	public required ProjectionVersion EngineVersion { get; init; }

	/// <summary>
	/// Target KurrentDB version. <c>null</c> (default) means "unversioned":
	/// gaffer reproduces every known upstream quirk and permits every feature,
	/// matching prod warts and all. Set explicitly to opt out of quirks that
	/// have been fixed upstream as of the given version.
	/// </summary>
	public KurrentDbVersion? QuirksVersion { get; init; }

	/// <summary>Maximum time for JS compilation. Default: 5 seconds.</summary>
	public TimeSpan CompilationTimeout { get; init; } = TimeSpan.FromSeconds(5);

	/// <summary>Maximum time for JS handler execution per event. Default: 5 seconds.</summary>
	public TimeSpan ExecutionTimeout { get; init; } = TimeSpan.FromSeconds(5);

	/// <summary>Enable Jint debug hooks for breakpoints and stepping. Has performance overhead.</summary>
	public bool Debug { get; init; }

	/// <summary>
	/// Populate <see cref="ProjectionInfo.Shape"/> by walking the AST
	/// with <see cref="Gaffer.Runtime.Projection.ShapeCollector"/>.
	/// Off by default - only telemetry-emitting paths (CLI dev / mcp
	/// commands when opt-out isn't active) set this. The LSP and
	/// other ProjectionInfo consumers leave it off and pay zero
	/// walker cost.
	/// </summary>
	public bool IncludeShape { get; init; }
}
