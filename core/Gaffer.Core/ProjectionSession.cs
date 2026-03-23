using System.Diagnostics;
using Gaffer.Core.Events;
using Gaffer.Core.Projection;

namespace Gaffer.Core;

public sealed class ProjectionSession : IDisposable
{
    private readonly JintProjectionHandler _handler;
    private readonly QuerySources _sources;
    private readonly Dictionary<string, string?> _partitionStates = new();
    private readonly TimeSpan _handlerTimeout;
    private readonly Stopwatch _handlerStopwatch = new();

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
            opts.CompilationTimeout,
            opts.ExecutionTimeout,
            message => OnLog?.Invoke(message));

        _sources = _handler.GetSourceDefinition();
    }

    public void Dispose() => _handler.Dispose();

    public void Feed(ProjectionEvent @event)
    {
        var partition = ResolvePartition(@event);
        EnsurePartitionInitialized(partition);
        LoadPartitionState(partition);

        if (!ShouldProcess(@event))
            return;

        _handlerStopwatch.Restart();
        _handler.ProcessEvent(
            partition,
            ResolveCategory(@event),
            @event,
            out var newState,
            out var newSharedState,
            out var emittedEvents);
        _handlerStopwatch.Stop();

        if (_handlerStopwatch.Elapsed > _handlerTimeout)
            OnSlowHandler?.Invoke(@event.EventType, (int)_handlerStopwatch.ElapsedMilliseconds);

        if (newState != null)
        {
            _partitionStates[partition] = newState;
            OnStateChanged?.Invoke(partition, newState);
        }

        if (newSharedState != null)
        {
            _partitionStates[""] = newSharedState;
        }

        if (emittedEvents != null)
        {
            foreach (var emitted in emittedEvents)
                OnEmit?.Invoke(emitted);
        }
    }

    public string? GetState(string? partition = null) =>
        _partitionStates.TryGetValue(partition ?? "", out var state) ? state : null;

    public void SetState(string? partition, string stateJson)
    {
        _partitionStates[partition ?? ""] = stateJson;
    }

    public string? GetResult(string? partition = null)
    {
        var key = partition ?? "";
        EnsurePartitionInitialized(key);
        LoadPartitionState(key);
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

    private bool ShouldProcess(ProjectionEvent @event)
    {
        if (_sources.AllEvents) return true;
        if (_sources.Events == null) return true;
        return _sources.Events.Contains(@event.EventType);
    }

    private readonly HashSet<string> _initializedPartitions = new();

    private void EnsurePartitionInitialized(string partition)
    {
        if (_initializedPartitions.Contains(partition))
            return;

        _initializedPartitions.Add(partition);
    }

    private void LoadPartitionState(string partition)
    {
        _handler.Initialize();
        if (_sources.IsBiState)
            _handler.InitializeShared();

        if (_partitionStates.TryGetValue(partition, out var state))
            _handler.Load(state);

        if (_sources.IsBiState && _partitionStates.TryGetValue("", out var sharedState))
            _handler.LoadShared(sharedState);
    }
}

public sealed class ProjectionSessionOptions
{
    public int HandlerTimeoutMs { get; init; } = 250;
    public TimeSpan CompilationTimeout { get; init; } = TimeSpan.FromSeconds(5);
    public TimeSpan ExecutionTimeout { get; init; } = TimeSpan.FromSeconds(5);
    public bool Debug { get; init; }
}
