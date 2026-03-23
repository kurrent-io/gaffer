using System.Text.Json;
using Gaffer.Runtime.Errors;
using Gaffer.Runtime.Events;
using Gaffer.Runtime.Projection;

namespace Gaffer.Runtime.Tests;

public class FixtureTests {
	private static readonly string FixturesDir = Path.Combine(
		AppContext.BaseDirectory, "..", "..", "..", "..", "..", "tools", "fixtures");

	private static IEnumerable<object[]> LoadFixtures(string filename) {
		var path = Path.Combine(FixturesDir, filename);
		var json = File.ReadAllText(path);
		var fixtures = JsonSerializer.Deserialize<JsonElement[]>(json)!;
		foreach (var fixture in fixtures)
			yield return [fixture.GetProperty("name").GetString()!, fixture];
	}

	public static IEnumerable<object[]> Sources => LoadFixtures("sources.json");
	public static IEnumerable<object[]> State => LoadFixtures("state.json");
	public static IEnumerable<object[]> Callbacks => LoadFixtures("callbacks.json");
	public static IEnumerable<object[]> Errors => LoadFixtures("errors.json");
	public static IEnumerable<object[]> Transforms => LoadFixtures("transforms.json");
	public static IEnumerable<object[]> Deletion => LoadFixtures("deletion.json");
	public static IEnumerable<object[]> Versioning => LoadFixtures("versioning.json");

	[Theory]
	[MemberData(nameof(Sources))]
	public void Sources_fixture(string _, JsonElement fixture) => RunFixture(fixture);

	[Theory]
	[MemberData(nameof(State))]
	public void State_fixture(string _, JsonElement fixture) => RunFixture(fixture);

	[Theory]
	[MemberData(nameof(Callbacks))]
	public void Callbacks_fixture(string _, JsonElement fixture) => RunFixture(fixture);

	[Theory]
	[MemberData(nameof(Errors))]
	public void Errors_fixture(string _, JsonElement fixture) => RunFixture(fixture);

	[Theory]
	[MemberData(nameof(Transforms))]
	public void Transforms_fixture(string _, JsonElement fixture) => RunFixture(fixture);

	[Theory]
	[MemberData(nameof(Deletion))]
	public void Deletion_fixture(string _, JsonElement fixture) => RunFixture(fixture);

	[Theory]
	[MemberData(nameof(Versioning))]
	public void Versioning_fixture(string _, JsonElement fixture) => RunFixture(fixture);

	private static void RunFixture(JsonElement fixture) {
		var source = fixture.GetProperty("source").GetString()!;
		var expect = fixture.GetProperty("expect");

		ProjectionSessionOptions? options = null;
		if (fixture.TryGetProperty("options", out var optionsEl))
			options = ParseOptions(optionsEl);

		if (expect.TryGetProperty("error", out var errorEl) && !fixture.TryGetProperty("events", out _) && !expect.TryGetProperty("getResult", out _)) {
			var ex = Assert.ThrowsAny<GafferException>(() => new ProjectionSession(source, options));
			AssertError(errorEl, ex);
			return;
		}

		using var session = new ProjectionSession(source, options);

		if (expect.TryGetProperty("sources", out var sourcesEl))
			AssertSources(session.Sources, sourcesEl);

		if (fixture.TryGetProperty("setState", out var setStateEl)) {
			var partition = setStateEl.GetProperty("partition");
			var stateJson = setStateEl.GetProperty("state").GetString()!;
			session.SetState(
				partition.ValueKind == JsonValueKind.Null ? null : partition.GetString(),
				stateJson);
		}

		var lastEmitted = new List<EmittedEvent>();
		var lastLogs = new List<string>();

		session.OnEmit = emitted => lastEmitted.Add(emitted);
		session.OnLog = message => lastLogs.Add(message);

		if (fixture.TryGetProperty("events", out var eventsEl)) {
			GafferException? lastFeedError = null;
			foreach (var ev in eventsEl.EnumerateArray()) {
				lastEmitted.Clear();
				lastLogs.Clear();

				var projectionEvent = new ProjectionEvent {
					EventType = ev.GetProperty("eventType").GetString()!,
					StreamId = ev.GetProperty("streamId").GetString()!,
					SequenceNumber = ev.GetProperty("sequenceNumber").GetInt64(),
					Data = ev.TryGetProperty("data", out var data) ? data.GetString() : "{}",
					IsJson = !ev.TryGetProperty("isJson", out var isJson) || isJson.GetBoolean(),
					Metadata = ev.TryGetProperty("metadata", out var metadata) ? metadata.GetString() : null,
				};

				try {
					session.Feed(projectionEvent);
				} catch (GafferException ex) {
					lastFeedError = ex;
				}
			}

			if (expect.TryGetProperty("error", out var feedErrorEl) && !expect.TryGetProperty("getResult", out _)) {
				Assert.NotNull(lastFeedError);
				AssertError(feedErrorEl, lastFeedError);
				return;
			}
		}

		if (expect.TryGetProperty("getResult", out _)) {
			var resultEx = Assert.ThrowsAny<GafferException>(() => session.GetResult());
			if (expect.TryGetProperty("error", out var resultErrorEl))
				AssertError(resultErrorEl, resultEx);
			return;
		}

		if (expect.TryGetProperty("state", out var stateEl)) {
			var state = session.GetState();
			Assert.NotNull(state);
			AssertJsonEqual(stateEl, JsonDocument.Parse(state).RootElement);
		}

		if (expect.TryGetProperty("states", out var statesEl)) {
			foreach (var prop in statesEl.EnumerateObject()) {
				var state = session.GetState(prop.Name);
				if (prop.Value.ValueKind == JsonValueKind.Null) {
					Assert.Null(state);
				} else {
					Assert.NotNull(state);
					AssertJsonEqual(prop.Value, JsonDocument.Parse(state).RootElement);
				}
			}
		}

		if (expect.TryGetProperty("sharedState", out var sharedEl)) {
			var shared = session.GetSharedState();
			Assert.NotNull(shared);
			AssertJsonEqual(sharedEl, JsonDocument.Parse(shared).RootElement);
		}

		if (expect.TryGetProperty("result", out var resultEl)) {
			if (resultEl.ValueKind == JsonValueKind.Null) {
				Assert.Null(session.GetResult());
			} else {
				var result = session.GetResult();
				Assert.NotNull(result);
				AssertJsonEqual(resultEl, JsonDocument.Parse(result).RootElement);
			}
		}

		if (expect.TryGetProperty("emitted", out var emittedEl)) {
			var expected = emittedEl.EnumerateArray().ToList();
			Assert.Equal(expected.Count, lastEmitted.Count);
			for (var i = 0; i < expected.Count; i++) {
				var exp = expected[i];
				var act = lastEmitted[i];
				Assert.Equal(exp.GetProperty("streamId").GetString(), act.StreamId);
				Assert.Equal(exp.GetProperty("eventType").GetString(), act.EventType);
				if (exp.TryGetProperty("data", out var emitData))
					Assert.Equal(emitData.GetString(), act.Data);
			}
		}

		if (expect.TryGetProperty("logs", out var logsEl)) {
			var expected = logsEl.EnumerateArray().Select(l => l.GetString()!).ToList();
			Assert.Equal(expected, lastLogs);
		}
	}

	private static ProjectionVersion ParseVersion(JsonElement el) {
		if (!el.TryGetProperty("version", out var v))
			return ProjectionVersion.V2;
		return v.GetString() switch {
			"v1" => ProjectionVersion.V1,
			"v2" => ProjectionVersion.V2,
			_ => throw new ArgumentException($"Unknown version: \"{v.GetString()}\""),
		};
	}

	private static ProjectionSessionOptions ParseOptions(JsonElement el) {
		return new ProjectionSessionOptions {
			Version = ParseVersion(el),
			HandlerTimeoutMs = el.TryGetProperty("handlerTimeoutMs", out var handlerTimeout)
				? handlerTimeout.GetInt32()
				: 250,
			CompilationTimeout = el.TryGetProperty("compilationTimeoutMs", out var compTimeout)
				? TimeSpan.FromMilliseconds(compTimeout.GetInt32())
				: TimeSpan.FromSeconds(5),
			ExecutionTimeout = el.TryGetProperty("executionTimeoutMs", out var execTimeout)
				? TimeSpan.FromMilliseconds(execTimeout.GetInt32())
				: TimeSpan.FromSeconds(5),
		};
	}

	private static void AssertSources(QuerySources sources, JsonElement expected) {
		foreach (var prop in expected.EnumerateObject()) {
			switch (prop.Name) {
				case "AllStreams":
					Assert.Equal(prop.Value.GetBoolean(), sources.AllStreams);
					break;
				case "AllEvents":
					Assert.Equal(prop.Value.GetBoolean(), sources.AllEvents);
					break;
				case "Streams":
					if (prop.Value.ValueKind == JsonValueKind.Null)
						Assert.Null(sources.Streams);
					else
						Assert.Equal(
							prop.Value.EnumerateArray().Select(s => s.GetString()!).ToArray(),
							sources.Streams);
					break;
				case "Categories":
					if (prop.Value.ValueKind == JsonValueKind.Null)
						Assert.Null(sources.Categories);
					else
						Assert.Equal(
							prop.Value.EnumerateArray().Select(s => s.GetString()!).ToArray(),
							sources.Categories);
					break;
				case "Events":
					if (prop.Value.ValueKind == JsonValueKind.Null)
						Assert.Null(sources.Events);
					else
						Assert.Equal(
							prop.Value.EnumerateArray().Select(s => s.GetString()!).ToArray(),
							sources.Events);
					break;
				case "ByStreams":
					Assert.Equal(prop.Value.GetBoolean(), sources.ByStreams);
					break;
				case "ByCustomPartitions":
					Assert.Equal(prop.Value.GetBoolean(), sources.ByCustomPartitions);
					break;
				case "IsBiState":
					Assert.Equal(prop.Value.GetBoolean(), sources.IsBiState);
					break;
				case "DefinesFold":
					Assert.Equal(prop.Value.GetBoolean(), sources.DefinesFold);
					break;
				case "DefinesStateTransform":
					Assert.Equal(prop.Value.GetBoolean(), sources.DefinesStateTransform);
					break;
				case "ProducesResults":
					Assert.Equal(prop.Value.GetBoolean(), sources.ProducesResults);
					break;
				case "HandlesDeletedNotifications":
					Assert.Equal(prop.Value.GetBoolean(), sources.HandlesDeletedNotifications);
					break;
				case "IncludeLinks":
					Assert.Equal(prop.Value.GetBoolean(), sources.IncludeLinks);
					break;
				case "ReorderEvents":
					Assert.Equal(prop.Value.GetBoolean(), sources.ReorderEvents);
					break;
				case "ProcessingLag":
					if (prop.Value.ValueKind == JsonValueKind.Null)
						Assert.Null(sources.ProcessingLag);
					else
						Assert.Equal(prop.Value.GetInt32(), sources.ProcessingLag);
					break;
			}
		}
	}

	private static void AssertError(JsonElement expected, GafferException actual) {
		if (expected.TryGetProperty("code", out var code))
			Assert.Equal(code.GetString(), actual.Code);
		if (expected.TryGetProperty("description", out var desc))
			Assert.Contains(desc.GetString()!, actual.Description);
	}

	private static void AssertJsonEqual(JsonElement expected, JsonElement actual) {
		Assert.Equal(
			JsonSerializer.Serialize(expected),
			JsonSerializer.Serialize(actual));
	}
}
