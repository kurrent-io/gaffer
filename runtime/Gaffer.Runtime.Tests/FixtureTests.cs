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

		if (!fixture.TryGetProperty("options", out var optionsEl))
			throw new ArgumentException($"Fixture {fixture.GetProperty("name")} is missing required \"options\" with engineVersion.");
		var options = ParseOptions(optionsEl);

		if (expect.TryGetProperty("error", out var errorEl) && !fixture.TryGetProperty("events", out _) && !expect.TryGetProperty("getResult", out _)) {
			var ex = Assert.ThrowsAny<ProjectionException>(() => new ProjectionSession(source, options));
			AssertError(errorEl, ex);
			return;
		}

		using var session = new ProjectionSession(source, options);

		if (expect.TryGetProperty("sources", out var sourcesEl))
			AssertSources(session.Sources, sourcesEl);

		if (expect.TryGetProperty("diagnostics", out var diagnosticsEl))
			AssertDiagnostics(session.Diagnostics, diagnosticsEl);

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
			ProjectionException? lastFeedError = null;
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
				} catch (ProjectionException ex) {
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
			var resultEx = Assert.ThrowsAny<ProjectionException>(() => session.GetResult());
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

	private static ProjectionVersion ParseEngineVersion(JsonElement el) {
		if (!el.TryGetProperty("engineVersion", out var v))
			throw new ArgumentException("Fixture options must include engineVersion (1 or 2)");
		return v.GetInt32() switch {
			1 => ProjectionVersion.V1,
			2 => ProjectionVersion.V2,
			var n => throw new ArgumentException($"Unknown engineVersion: {n}"),
		};
	}

	private static ProjectionSessionOptions ParseOptions(JsonElement el) {
		return new ProjectionSessionOptions {
			EngineVersion = ParseEngineVersion(el),
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
				case "allStreams":
					Assert.Equal(prop.Value.GetBoolean(), sources.AllStreams);
					break;
				case "allEvents":
					Assert.Equal(prop.Value.GetBoolean(), sources.AllEvents);
					break;
				case "streams":
					if (prop.Value.ValueKind == JsonValueKind.Null)
						Assert.Null(sources.Streams);
					else
						Assert.Equal(
							prop.Value.EnumerateArray().Select(s => s.GetString()!).ToArray(),
							sources.Streams);
					break;
				case "categories":
					if (prop.Value.ValueKind == JsonValueKind.Null)
						Assert.Null(sources.Categories);
					else
						Assert.Equal(
							prop.Value.EnumerateArray().Select(s => s.GetString()!).ToArray(),
							sources.Categories);
					break;
				case "events":
					if (prop.Value.ValueKind == JsonValueKind.Null)
						Assert.Null(sources.Events);
					else
						Assert.Equal(
							prop.Value.EnumerateArray().Select(s => s.GetString()!).ToArray(),
							sources.Events);
					break;
				case "byStreams":
					Assert.Equal(prop.Value.GetBoolean(), sources.ByStreams);
					break;
				case "byCustomPartitions":
					Assert.Equal(prop.Value.GetBoolean(), sources.ByCustomPartitions);
					break;
				case "biState":
					Assert.Equal(prop.Value.GetBoolean(), sources.IsBiState);
					break;
				case "definesHandlers":
					Assert.Equal(prop.Value.GetBoolean(), sources.DefinesFold);
					break;
				case "definesStateTransform":
					Assert.Equal(prop.Value.GetBoolean(), sources.DefinesStateTransform);
					break;
				case "producesResults":
					Assert.Equal(prop.Value.GetBoolean(), sources.ProducesResults);
					break;
				case "handlesDeletedNotifications":
					Assert.Equal(prop.Value.GetBoolean(), sources.HandlesDeletedNotifications);
					break;
				case "includeLinks":
					Assert.Equal(prop.Value.GetBoolean(), sources.IncludeLinks);
					break;
				case "reorderEvents":
					Assert.Equal(prop.Value.GetBoolean(), sources.ReorderEvents);
					break;
				case "processingLag":
					if (prop.Value.ValueKind == JsonValueKind.Null)
						Assert.Null(sources.ProcessingLag);
					else
						Assert.Equal(prop.Value.GetInt32(), sources.ProcessingLag);
					break;
				default:
					throw new ArgumentException($"Unknown sources key in fixture: {prop.Name}");
			}
		}
	}

	// Strict diagnostic assertion: count must match, every expected entry
	// must appear (matched by code, with optional severity check). Strict
	// rather than contains so accidental new diagnostics don't slip in
	// silently.
	private static void AssertDiagnostics(Sdk.Diagnostics.Diagnostic[]? actual, JsonElement expected) {
		var expectedList = expected.EnumerateArray().ToList();
		var actualList = actual ?? Array.Empty<Sdk.Diagnostics.Diagnostic>();
		Assert.Equal(expectedList.Count, actualList.Length);
		foreach (var exp in expectedList) {
			var code = exp.GetProperty("code").GetString()!;
			var match = actualList.FirstOrDefault(d => d.Code == code);
			Assert.NotNull(match);
			if (exp.TryGetProperty("severity", out var sevEl)) {
				var expectedSeverity = ParseSeverity(sevEl.GetString()!);
				Assert.Equal(expectedSeverity, match!.Severity);
			}
		}
	}

	private static Sdk.Diagnostics.DiagnosticSeverity ParseSeverity(string s) => s switch {
		"error" => Sdk.Diagnostics.DiagnosticSeverity.Error,
		"warning" => Sdk.Diagnostics.DiagnosticSeverity.Warning,
		"information" => Sdk.Diagnostics.DiagnosticSeverity.Information,
		"hint" => Sdk.Diagnostics.DiagnosticSeverity.Hint,
		_ => throw new ArgumentException($"Unknown severity in fixture: {s} (expected error|warning|information|hint)"),
	};

	private static void AssertError(JsonElement expected, ProjectionException actual) {
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
