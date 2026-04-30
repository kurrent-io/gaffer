using Gaffer.Runtime.Errors;
using Gaffer.Runtime.Events;
namespace Gaffer.Runtime.Tests;

public class ErrorTests {
	private static readonly ProjectionEvent TestEvent = new() {
		EventType = "Test",
		StreamId = "s-1",
		SequenceNumber = 42,
		Data = "{}",
		IsJson = true,
	};

	[Fact]
	public Task InvalidProjectionError_parse_error_with_location() {
		var source = "this is not valid {{{{";
		var ex = Assert.Throws<InvalidProjectionException>(() => new ProjectionSession(source, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 }));

		Assert.Equal("invalid-projection", ex.Code);
		Assert.NotEmpty(ex.Description);
		Assert.Equal(source, ex.ProjectionSource);
		Assert.NotNull(ex.Line);
		Assert.NotNull(ex.Column);

		return VerifyXunit.Verifier.Verify(ex.Message);
	}

	[Fact]
	public Task InvalidProjectionError_source_definition_error() {
		var ex = Assert.Throws<InvalidProjectionException>(() => new ProjectionSession("fromStream(123)", new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 }));

		Assert.Equal("invalid-projection", ex.Code);
		Assert.Equal("fromStream expects a string argument", ex.Description);
		Assert.Null(ex.Line);
		Assert.Null(ex.Column);

		return VerifyXunit.Verifier.Verify(ex.Message);
	}

	[Fact]
	public Task CompilationTimeoutError() {
		var ex = Assert.Throws<CompilationTimeoutException>(() =>
			new ProjectionSession("while(true) {}", new ProjectionSessionOptions {
				EngineVersion = ProjectionVersion.V2,
				CompilationTimeout = TimeSpan.FromMilliseconds(100),
			}));

		Assert.Equal("compilation-timeout", ex.Code);
		Assert.Contains("compile", ex.Description);
		Assert.True(ex.ElapsedMs > 0);
		Assert.Equal(100, ex.AllowedMs);

		return VerifyXunit.Verifier.Verify(ex.Message);
	}

	[Fact]
	public Task ProjectionHandlerError_with_event_context() {
		var source = "fromAll().when({\n\t$init: function() { return {}; },\n\tTest: function(s, e) { throw new Error(\"boom\"); }\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		var ex = Assert.Throws<ProjectionHandlerException>(() => session.Feed(TestEvent));

		Assert.Equal("handler-error", ex.Code);
		Assert.Equal("boom", ex.Description);
		Assert.Equal(source, ex.ProjectionSource);
		Assert.Equal("Test", ex.EventType);
		Assert.Equal("s-1", ex.StreamId);
		Assert.Equal(42, ex.SequenceNumber);
		Assert.Null(ex.Partition);

		return VerifyXunit.Verifier.Verify(ex.Message);
	}

	[Fact]
	public Task ProjectionHandlerError_with_partition() {
		var source = "fromAll().foreachStream().when({\n\t$init: function() { return {}; },\n\tTest: function(s, e) { throw \"fail\"; }\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		var ex = Assert.Throws<ProjectionHandlerException>(() => session.Feed(TestEvent));

		Assert.Equal("s-1", ex.Partition);

		return VerifyXunit.Verifier.Verify(ex.Message);
	}

	[Fact]
	public Task ExecutionTimeoutError() {
		var source = "fromAll().when({\n\t$init: function() { return {}; },\n\tTest: function(s, e) { while(true) {} }\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions {
			EngineVersion = ProjectionVersion.V2,
			ExecutionTimeout = TimeSpan.FromMilliseconds(100),
		});

		var ex = Assert.Throws<ExecutionTimeoutException>(() => session.Feed(TestEvent));

		Assert.Equal("execution-timeout", ex.Code);
		Assert.Contains("execute", ex.Description);
		Assert.True(ex.ElapsedMs > 0);
		Assert.Equal(100, ex.AllowedMs);
		Assert.Equal("Test", ex.EventType);
		Assert.Equal("s-1", ex.StreamId);
		Assert.Equal(42, ex.SequenceNumber);

		return VerifyXunit.Verifier.Verify(ex.Message);
	}

	[Fact]
	public Task MalformedEventError_isJson_with_invalid_data() {
		var source = "fromAll().when({\n\t$init: function() { return {}; },\n\tTest: function(s, e) { return e.data; }\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		var ex = Assert.Throws<MalformedEventException>(() =>
			session.Feed(new ProjectionEvent {
				EventType = "Test",
				StreamId = "s-1",
				SequenceNumber = 42,
				Data = "not json",
				IsJson = true,
			}));

		Assert.Equal("malformed-event", ex.Code);
		Assert.Contains("not valid JSON", ex.Description);
		Assert.Equal("Test", ex.EventType);
		Assert.Equal("s-1", ex.StreamId);
		Assert.Equal(42, ex.SequenceNumber);

		return VerifyXunit.Verifier.Verify(ex.Message);
	}

	[Fact]
	public Task StateSerializationError_NaN() {
		var source = "fromAll().when({\n\t$init: function() { return {}; },\n\tTest: function(s, e) { s.value = NaN; return s; }\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		var ex = Assert.Throws<StateSerializationException>(() => session.Feed(TestEvent));

		Assert.Equal("state-serialization-error", ex.Code);
		Assert.Contains("NaN", ex.Description);
		Assert.Equal("Test", ex.EventType);
		Assert.Equal("s-1", ex.StreamId);
		Assert.Equal(42, ex.SequenceNumber);

		return VerifyXunit.Verifier.Verify(ex.Message);
	}

	[Fact]
	public Task ProjectionTransformError() {
		var source = "fromAll().when({\n\t$init: function() { return {}; },\n\tTest: function(s, e) { return s; }\n}).transformBy(function(s) {\n\tthrow new Error(\"transform failed\");\n}).outputState()";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		var ex = Assert.Throws<ProjectionTransformException>(() => session.Feed(TestEvent));

		Assert.Equal("projection-transform-error", ex.Code);
		Assert.Equal("transform failed", ex.Description);
		Assert.Equal(source, ex.ProjectionSource);

		return VerifyXunit.Verifier.Verify(ex.Message);
	}
}
