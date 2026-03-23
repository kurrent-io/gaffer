namespace Gaffer.Runtime.Projection;

internal sealed class SourceDefinitionBuilder {
	private bool _allStreams;
	private List<string>? _categories;
	private List<string>? _streams;
	private bool _allEvents;
	private List<string>? _events;
	private bool _byStream;
	private bool _byCustomPartitions;
	private bool _definesFold = true;
	private bool _definesStateTransform;
	private bool _producesResults;
	private bool _handlesDeletedNotifications;
	private bool _includeLinks;
	private bool _isBiState;
	private string? _resultStreamName;
	private string? _partitionResultStreamNamePattern;
	private bool _reorderEvents;
	private int? _processingLag;

	public bool IsBiState => _isBiState;

	public void FromAll() => _allStreams = true;

	public void FromCategory(string categoryName) {
		_categories ??= new List<string>();
		_categories.Add(categoryName);
	}

	public void FromStream(string streamName) {
		_streams ??= new List<string>();
		_streams.Add(streamName);
	}

	public void AllEvents() => _allEvents = true;
	public void NotAllEvents() => _allEvents = false;
	public void SetIncludeLinks(bool includeLinks = true) => _includeLinks = includeLinks;

	public void IncludeEvent(string eventName) {
		_events ??= new List<string>();
		_events.Add(eventName);
	}

	public void SetByStream() => _byStream = true;
	public void SetByCustomPartitions() => _byCustomPartitions = true;
	public void SetDefinesStateTransform() => _definesStateTransform = true;
	public void SetOutputState() => _producesResults = true;
	public void NoWhen() => _definesFold = false;
	public void SetDefinesFold() => _definesFold = true;
	public void SetHandlesStreamDeletedNotifications(bool value = true) => _handlesDeletedNotifications = value;
	public void SetIsBiState(bool isBiState) => _isBiState = isBiState;
	public void SetReorderEvents(bool reorderEvents) => _reorderEvents = reorderEvents;
	public void SetProcessingLag(int processingLag) => _processingLag = processingLag;

	public void SetResultStreamNameOption(string resultStreamName) =>
		_resultStreamName = string.IsNullOrWhiteSpace(resultStreamName) ? null : resultStreamName;

	public void SetPartitionResultStreamNamePatternOption(string pattern) =>
		_partitionResultStreamNamePattern = string.IsNullOrWhiteSpace(pattern) ? null : pattern;

	public QuerySources Build() => new() {
		AllStreams = _allStreams,
		AllEvents = _allEvents,
		Categories = _categories?.ToArray(),
		Streams = _streams?.ToArray(),
		Events = _events?.ToArray(),
		ByStreams = _byStream,
		ByCustomPartitions = _byCustomPartitions,
		IsBiState = _isBiState,
		DefinesFold = _definesFold,
		DefinesStateTransform = _definesStateTransform,
		ProducesResults = _producesResults,
		HandlesDeletedNotifications = _handlesDeletedNotifications,
		IncludeLinks = _includeLinks,
		ResultStreamName = _resultStreamName,
		PartitionResultStreamNamePattern = _partitionResultStreamNamePattern,
		ReorderEvents = _reorderEvents,
		ProcessingLag = _processingLag,
	};
}
