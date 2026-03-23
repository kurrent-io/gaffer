using System.Diagnostics;
using System.Text.Json;
using Gaffer.Core.Events;
using Gaffer.Core.Projection;

namespace Gaffer.Core;

public sealed class ProjectionSession : IDisposable
{
    private readonly JintProjectionHandler _handler;
    private readonly QuerySources _sources;
    private readonly Dictionary<string, string?> _stateCache = new();
    private readonly HashSet<string>? _handledEventTypes;
    private string? _sharedState;
    private readonly TimeSpan _handlerTimeout;
    private readonly Stopwatch _handlerStopwatch = new();
    private bool _sharedStateInitialized;

    public Action<EmittedEvent>? OnEmit { get; set; }
    public Action<string>? OnLog { get; set; }
    public Action<string, string?>? OnStateChanged { get; set; }
    public Action<string, int>? OnSlowHandler { get; set; }

    public QuerySources Sources => _sources;

    public ProjectionSession(string source, ProjectionSessionOptions? options = null)
    {
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

    public const string StreamDeletedEventType = "$streamDeleted";
    public const string StreamMetadataEventType = "$metadata";
    private const string MetastreamPrefix = "$$";

    public void Feed(ProjectionEvent @event)
    {
        if (IsStreamDeletedEvent(@event, out var deletedStreamId))
        {
            FeedStreamDeleted(deletedStreamId);
            return;
        }

        var partition = ResolvePartition(@event);

        if (!ShouldProcess(@event))
            return;

        var isNewPartition = LoadPartitionState(partition);
        LoadSharedState();

        if (isNewPartition)
        {
            _handler.ProcessPartitionCreated(partition, @event, out var createdEmitted);
            if (createdEmitted != null)
                foreach (var e in createdEmitted)
                    OnEmit?.Invoke(e);
        }

        _handlerStopwatch.Restart();
        try
        {
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
        }
        catch (Exception ex) when (ex is not ProjectionException)
        {
            _handlerStopwatch.Stop();
            throw new ProjectionException(
                $"Error processing {@event.EventType} in partition '{partition}': {ex.Message}", ex);
        }
    }

    private static bool IsStreamDeletedEvent(ProjectionEvent @event, out string deletedStreamId)
    {
        if (@event.EventType == StreamDeletedEventType)
        {
            deletedStreamId = @event.StreamId;
            return true;
        }

        if (@event.EventType == StreamMetadataEventType &&
            @event.StreamId.StartsWith(MetastreamPrefix) &&
            @event.Data != null)
        {
            // Soft delete: $metadata on $$streamId with $tb set to long.MaxValue (EventNumber.DeletedStream)
            // Matches KurrentDB: throws on malformed metadata rather than ignoring
            using var doc = JsonDocument.Parse(@event.Data);
            if (doc.RootElement.TryGetProperty("$tb", out var tb) &&
                tb.ValueKind == JsonValueKind.Number &&
                tb.GetInt64() == long.MaxValue)
            {
                deletedStreamId = @event.StreamId[MetastreamPrefix.Length..];
                return true;
            }
        }

        deletedStreamId = "";
        return false;
    }

    private void FeedStreamDeleted(string partition)
    {
        if (!_sources.HandlesDeletedNotifications)
            return;

        LoadPartitionState(partition);
        LoadSharedState();

        _handler.ProcessPartitionDeleted(partition, out var newState);

        _stateCache[partition] = newState;

        if (newState != null)
            OnStateChanged?.Invoke(partition, newState);
    }

    private void ProcessOutput(string partition, ProjectionEvent @event,
        string? newState, string? newSharedState, EmittedEvent[]? emittedEvents)
    {

        if (_handlerStopwatch.Elapsed > _handlerTimeout)
            OnSlowHandler?.Invoke(@event.EventType, (int)_handlerStopwatch.ElapsedMilliseconds);

        // Cache state (even if null - tracks that partition has been seen)
        _stateCache[partition] = newState;

        if (newState != null)
            OnStateChanged?.Invoke(partition, newState);

        if (_sources.IsBiState && newSharedState != null)
            _sharedState = newSharedState;

        if (emittedEvents != null)
            foreach (var emitted in emittedEvents)
                OnEmit?.Invoke(emitted);
    }

    public string? GetState(string? partition = null) =>
        _stateCache.TryGetValue(partition ?? "", out var state) ? state : null;

    public string? GetSharedState() => _sharedState;

    public void SetState(string? partition, string stateJson)
    {
        _stateCache[partition ?? ""] = stateJson;
    }

    public string? GetResult(string? partition = null)
    {
        var key = partition ?? "";
        if (!_stateCache.ContainsKey(key))
            return null;
        LoadPartitionState(key);
        if (_sharedStateInitialized)
            LoadSharedState();
        return _handler.TransformStateToResult();
    }

    public string? GetPartitionKey(ProjectionEvent @event) => ResolvePartition(@event);

    private string ResolvePartition(ProjectionEvent @event)
    {
        if (_sources.ByCustomPartitions)
            return _handler.GetStatePartition(@event, ResolveCategory(@event)) ?? "";

        if (_sources.ByStreams)
            return @event.StreamId;

        return "";
    }

    private static string ResolveCategory(ProjectionEvent @event)
    {
        var streamId = @event.StreamId;
        var dashIndex = streamId.IndexOf('-');
        return dashIndex >= 0 ? streamId[..dashIndex] : streamId;
    }

    private bool ShouldProcess(ProjectionEvent @event) =>
        _handledEventTypes == null || _handledEventTypes.Contains(@event.EventType);

    /// <summary>
    /// Loads partition state from cache or initializes fresh.
    /// Returns true if the partition is new (not previously seen).
    /// Matches V2 PartitionProcessor.LoadPartitionState behavior.
    /// </summary>
    private bool LoadPartitionState(string partition)
    {
        // ContainsKey (not TryGetValue) - tracks partitions with null state too
        if (_stateCache.ContainsKey(partition))
        {
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
    /// Matches V2 PartitionProcessor.LoadSharedState behavior.
    /// </summary>
    private void LoadSharedState()
    {
        if (!_sources.IsBiState) return;

        if (!_sharedStateInitialized)
        {
            _handler.InitializeShared();
            _sharedStateInitialized = true;
        }
        else if (_sharedState != null)
        {
            _handler.LoadShared(_sharedState);
        }
    }
}

public sealed class ProjectionSessionOptions
{
    public int HandlerTimeoutMs { get; init; } = 250;
    public TimeSpan CompilationTimeout { get; init; } = TimeSpan.FromSeconds(5);
    public TimeSpan ExecutionTimeout { get; init; } = TimeSpan.FromSeconds(5);
    public bool Debug { get; init; }
    public bool EnableContentTypeValidation { get; init; }
}
