using System.Collections.Concurrent;
using System.Runtime.CompilerServices;
using System.Runtime.InteropServices;
using System.Text;
using System.Text.Json;
using Gaffer.Runtime.Errors;
using Gaffer.Runtime.Events;
using Gaffer.Runtime.Projection;
using Gaffer.Sdk;
using Gaffer.Sdk.Diagnostics;
using Gaffer.Sdk.Versioning;

namespace Gaffer.Runtime;

/// <summary>
/// C API exports for the gaffer runtime. These methods are callable from
/// native code (Go via cgo, Node via N-API, etc.) when built with NativeAOT.
/// </summary>
internal static unsafe class NativeExports {
	private static readonly ConcurrentDictionary<nint, SessionHandle> Sessions = new();
	private static long _nextHandle;

	private sealed class SessionHandle {
		public required ProjectionSession Session { get; init; }
	}

	private static string? FromUtf8(byte* ptr) {
		if (ptr == null)
			return null;
		return Marshal.PtrToStringUTF8((nint)ptr);
	}

	private static void WriteError(byte** errorOut, Exception ex) {
		if (errorOut == null)
			return;
		var json = ex is ProjectionException ge
			? SerializeProjectionError(ge)
			: SerializeUnexpectedError(ex);
		*errorOut = AllocUtf8(json);
	}

	internal static string SerializeProjectionError(ProjectionException ex) {
		using var stream = new System.IO.MemoryStream();
		using var writer = new Utf8JsonWriter(stream);
		writer.WriteStartObject();
		writer.WriteString("code", ex.Code);
		writer.WriteString("description", ex.Description);
		if (ex.Message != ex.Description)
			writer.WriteString("message", ex.Message);
		if (ex.CompatCode != null) {
			writer.WriteString("compatCode", ex.CompatCode);
			// Enrich at source from the catalog so consumers annotate the fatal error
			// ("reproduces upstream bug X, fixed in DB version Y") without a registry round-trip.
			if (Sdk.Diagnostics.DiagnosticCatalog.TryGet(ex.CompatCode, out var quirk)) {
				writer.WriteString("compatDescription", quirk.Message);
				if (quirk.FixedIn != null)
					writer.WriteString("compatFixedIn", quirk.FixedIn.ToString());
			}
		}

		switch (ex) {
			case InvalidProjectionException ip:
				if (ip.Line != null)
					writer.WriteNumber("line", ip.Line.Value);
				if (ip.Column != null)
					writer.WriteNumber("column", ip.Column.Value);
				break;
			case CompilationTimeoutException ct:
				writer.WriteNumber("elapsed", ct.ElapsedMs);
				writer.WriteNumber("allowed", ct.AllowedMs);
				break;
			case InvalidArgumentException ia:
				writer.WriteString("field", ia.Field);
				break;
			case ProjectionHandlerException ph:
				if (ph.JsStack != null)
					writer.WriteString("jsStack", ph.JsStack);
				if (ph.Line != null)
					writer.WriteNumber("line", ph.Line.Value);
				if (ph.Column != null)
					writer.WriteNumber("column", ph.Column.Value);
				writer.WriteString("eventType", ph.EventType);
				writer.WriteString("streamId", ph.StreamId);
				writer.WriteNumber("sequenceNumber", ph.SequenceNumber);
				if (ph.Partition != null)
					writer.WriteString("partition", ph.Partition);
				break;
			case ExecutionTimeoutException et:
				writer.WriteNumber("elapsed", et.ElapsedMs);
				writer.WriteNumber("allowed", et.AllowedMs);
				writer.WriteString("eventType", et.EventType);
				writer.WriteString("streamId", et.StreamId);
				writer.WriteNumber("sequenceNumber", et.SequenceNumber);
				if (et.Partition != null)
					writer.WriteString("partition", et.Partition);
				break;
			case MalformedEventException me:
				writer.WriteString("eventType", me.EventType);
				writer.WriteString("streamId", me.StreamId);
				writer.WriteNumber("sequenceNumber", me.SequenceNumber);
				if (me.Partition != null)
					writer.WriteString("partition", me.Partition);
				break;
			case StateSerializationException ss:
				writer.WriteString("eventType", ss.EventType);
				writer.WriteString("streamId", ss.StreamId);
				writer.WriteNumber("sequenceNumber", ss.SequenceNumber);
				if (ss.Partition != null)
					writer.WriteString("partition", ss.Partition);
				break;
			case ProjectionTransformException pt:
				if (pt.JsStack != null)
					writer.WriteString("jsStack", pt.JsStack);
				if (pt.Line != null)
					writer.WriteNumber("line", pt.Line.Value);
				if (pt.Column != null)
					writer.WriteNumber("column", pt.Column.Value);
				break;
		}

		// Quirks-always-diagnose: every quirk that fired while processing the throwing event,
		// including the throwing quirk itself, rides the error alongside the single compatCode.
		if (ex.Diagnostics.Count > 0) {
			writer.WritePropertyName("diagnostics");
			writer.WriteRawValue(JsonSerializer.Serialize(
				ex.Diagnostics.ToArray(), Sdk.SdkJsonContext.Default.DiagnosticArray));
		}

		writer.WriteEndObject();
		writer.Flush();
		return Encoding.UTF8.GetString(stream.ToArray());
	}

	private static string SerializeUnexpectedError(Exception ex) {
		using var stream = new System.IO.MemoryStream();
		using var writer = new Utf8JsonWriter(stream);
		writer.WriteStartObject();
		writer.WriteString("code", "unexpected");
		writer.WriteString("description", ex.Message);
		if (ex is Jint.Runtime.JavaScriptException jsEx) {
			if (jsEx.JavaScriptStackTrace != null)
				writer.WriteString("jsStack", jsEx.JavaScriptStackTrace);
		}
		writer.WriteEndObject();
		writer.Flush();
		return Encoding.UTF8.GetString(stream.ToArray());
	}

	private static string SerializeFeedResult(FeedResult result) {
		using var stream = new System.IO.MemoryStream();
		using var writer = new Utf8JsonWriter(stream);
		writer.WriteStartObject();

		if (result.Status == FeedStatus.Skipped) {
			writer.WriteString("status", "skipped");
			if (result.SkipReason != null)
				writer.WriteString("reason", result.SkipReason);
			writer.WriteEndObject();
			writer.Flush();
			return Encoding.UTF8.GetString(stream.ToArray());
		}

		writer.WriteString("status", "processed");
		if (result.Partition is { Length: > 0 })
			writer.WriteString("partition", result.Partition);

		if (result.State != null) {
			writer.WritePropertyName("state");
			writer.WriteRawValue(result.State);
		} else {
			writer.WriteNull("state");
		}

		if (result.Result != null) {
			writer.WritePropertyName("result");
			writer.WriteRawValue(result.Result);
		} else {
			writer.WriteNull("result");
		}

		if (result.SharedState != null) {
			writer.WritePropertyName("sharedState");
			writer.WriteRawValue(result.SharedState);
		} else {
			writer.WriteNull("sharedState");
		}

		writer.WriteStartArray("emitted");
		for (int i = 0; i < result.Emitted.Length; i++) {
			var e = result.Emitted[i];
			writer.WriteStartObject();
			writer.WriteString("streamId", e.StreamId);
			writer.WriteString("eventType", e.EventType);
			if (e.Data != null)
				writer.WriteString("data", e.Data);
			else
				writer.WriteNull("data");
			writer.WriteBoolean("isJson", e.IsJson);
			writer.WriteBoolean("isLink", e.IsLink);
			if (e.Metadata != null) {
				writer.WriteStartObject("metadata");
				foreach (var kvp in e.Metadata)
					writer.WriteString(kvp.Key, kvp.Value);
				writer.WriteEndObject();
			} else {
				writer.WriteNull("metadata");
			}
			writer.WriteEndObject();
		}
		writer.WriteEndArray();

		writer.WriteStartArray("logs");
		for (int i = 0; i < result.Logs.Length; i++)
			writer.WriteStringValue(result.Logs[i]);
		writer.WriteEndArray();

		writer.WritePropertyName("diagnostics");
		writer.WriteRawValue(JsonSerializer.Serialize(
			result.Diagnostics, Sdk.SdkJsonContext.Default.DiagnosticArray));

		writer.WriteEndObject();
		writer.Flush();
		return Encoding.UTF8.GetString(stream.ToArray());
	}

	// -- Session lifecycle --

	[UnmanagedCallersOnly(EntryPoint = "gaffer_session_create")]
	public static nint Create(byte* source, byte* optionsJson, byte** errorOut) {
		if (errorOut != null)
			*errorOut = null;
		try {
			var sourceStr = FromUtf8(source);
			if (sourceStr == null) {
				WriteError(errorOut, new InvalidArgumentException("source is null", "source"));
				return 0;
			}

			var opts = ParseOptions(FromUtf8(optionsJson));
			var session = new ProjectionSession(sourceStr, opts);

			var handle = new SessionHandle { Session = session };
			var id = (nint)Interlocked.Increment(ref _nextHandle);
			Sessions[id] = handle;
			return id;
		} catch (Exception ex) {
			WriteError(errorOut, ex);
			return 0;
		}
	}

	[UnmanagedCallersOnly(EntryPoint = "gaffer_session_destroy")]
	public static void Destroy(nint sessionId) {
		if (!Sessions.TryRemove(sessionId, out var handle))
			return;

		try {
			handle.Session.Dispose();
		} catch {
			// Best effort cleanup
		}
	}

	// -- Callbacks --

	[UnmanagedCallersOnly(EntryPoint = "gaffer_on_emit")]
	public static void OnEmit(nint sessionId, delegate* unmanaged<byte*, byte*, byte*, byte*, int, int, void*, void> cb, void* userData) {
		if (!Sessions.TryGetValue(sessionId, out var handle))
			return;

		handle.Session.OnEmit = emitted => {
			var streamId = AllocUtf8(emitted.StreamId);
			var eventType = AllocUtf8(emitted.EventType);
			var data = AllocUtf8(emitted.Data);
			var metadata = AllocUtf8(emitted.Metadata != null ? JsonSerializer.Serialize(emitted.Metadata, GafferJsonContext.Default.DictionaryStringString) : null);
			try {
				cb(streamId, eventType, data, metadata, emitted.IsJson ? 1 : 0, emitted.IsLink ? 1 : 0, userData);
			} finally {
				FreeUtf8(streamId);
				FreeUtf8(eventType);
				FreeUtf8(data);
				FreeUtf8(metadata);
			}
		};
	}

	[UnmanagedCallersOnly(EntryPoint = "gaffer_on_log")]
	public static void OnLog(nint sessionId, delegate* unmanaged<byte*, void*, void> cb, void* userData) {
		if (!Sessions.TryGetValue(sessionId, out var handle))
			return;

		handle.Session.OnLog = message => {
			var msg = AllocUtf8(message);
			try {
				cb(msg, userData);
			} finally {
				FreeUtf8(msg);
			}
		};
	}

	[UnmanagedCallersOnly(EntryPoint = "gaffer_on_diagnostic")]
	public static void OnDiagnostic(nint sessionId, delegate* unmanaged<byte*, byte*, int, void*, void> cb, void* userData) {
		if (!Sessions.TryGetValue(sessionId, out var handle))
			return;

		handle.Session.OnDiagnostic = diagnostic => {
			var code = AllocUtf8(diagnostic.Code);
			var message = AllocUtf8(diagnostic.Message);
			try {
				cb(code, message, (int)diagnostic.Severity, userData);
			} finally {
				FreeUtf8(code);
				FreeUtf8(message);
			}
		};
	}

	[UnmanagedCallersOnly(EntryPoint = "gaffer_on_state_changed")]
	public static void OnStateChanged(nint sessionId, delegate* unmanaged<byte*, byte*, void*, void> cb, void* userData) {
		if (!Sessions.TryGetValue(sessionId, out var handle))
			return;

		handle.Session.OnStateChanged = (partition, stateJson) => {
			var part = AllocUtf8(partition);
			var state = AllocUtf8(stateJson);
			try {
				cb(part, state, userData);
			} finally {
				FreeUtf8(part);
				FreeUtf8(state);
			}
		};
	}

	[UnmanagedCallersOnly(EntryPoint = "gaffer_on_break")]
	public static void OnBreak(nint sessionId, delegate* unmanaged<byte*, byte*, int, int, void*, void> cb, void* userData) {
		if (!Sessions.TryGetValue(sessionId, out var handle))
			return;

		handle.Session.OnBreak = info => {
			var reason = AllocUtf8(info.Reason);
			var source = AllocUtf8("projection.js");
			try {
				cb(reason, source, info.Line, info.Column, userData);
			} finally {
				FreeUtf8(reason);
				FreeUtf8(source);
			}
		};
	}

	// -- Event feeding --

	private static readonly InvalidArgumentException InvalidSessionError =
		new("Invalid session handle", "session");

	[UnmanagedCallersOnly(EntryPoint = "gaffer_session_feed")]
	public static byte* Feed(nint sessionId, byte* eventJson, byte** errorOut) {
		if (errorOut != null)
			*errorOut = null;
		if (!Sessions.TryGetValue(sessionId, out var handle)) {
			WriteError(errorOut, InvalidSessionError);
			return null;
		}

		try {
			var json = FromUtf8(eventJson);
			if (json == null) {
				WriteError(errorOut, new InvalidArgumentException("event_json is null", "event_json"));
				return null;
			}

			var evt = ParseEvent(json);
			var result = handle.Session.Feed(evt);
			return AllocUtf8(SerializeFeedResult(result));
		} catch (Exception ex) {
			WriteError(errorOut, ex);
			return null;
		}
	}

	// -- State --

	[UnmanagedCallersOnly(EntryPoint = "gaffer_session_get_state")]
	public static byte* GetState(nint sessionId, byte* partition, byte** errorOut) {
		if (errorOut != null)
			*errorOut = null;
		if (!Sessions.TryGetValue(sessionId, out var handle)) {
			WriteError(errorOut, InvalidSessionError);
			return null;
		}
		try {
			return AllocUtf8(handle.Session.GetState(FromUtf8(partition)));
		} catch (Exception ex) {
			WriteError(errorOut, ex);
			return null;
		}
	}

	[UnmanagedCallersOnly(EntryPoint = "gaffer_session_get_shared_state")]
	public static byte* GetSharedState(nint sessionId, byte** errorOut) {
		if (errorOut != null)
			*errorOut = null;
		if (!Sessions.TryGetValue(sessionId, out var handle)) {
			WriteError(errorOut, InvalidSessionError);
			return null;
		}
		try {
			return AllocUtf8(handle.Session.GetSharedState());
		} catch (Exception ex) {
			WriteError(errorOut, ex);
			return null;
		}
	}

	[UnmanagedCallersOnly(EntryPoint = "gaffer_session_set_state")]
	public static void SetState(nint sessionId, byte* partition, byte* stateJson, byte** errorOut) {
		if (errorOut != null)
			*errorOut = null;
		if (!Sessions.TryGetValue(sessionId, out var handle)) {
			WriteError(errorOut, InvalidSessionError);
			return;
		}
		try {
			var json = FromUtf8(stateJson);
			if (json == null) {
				WriteError(errorOut, new InvalidArgumentException("state_json is null", "state_json"));
				return;
			}
			handle.Session.SetState(FromUtf8(partition), json);
		} catch (Exception ex) {
			WriteError(errorOut, ex);
		}
	}

	[UnmanagedCallersOnly(EntryPoint = "gaffer_session_get_result")]
	public static byte* GetResult(nint sessionId, byte* partition, byte** errorOut) {
		if (errorOut != null)
			*errorOut = null;
		if (!Sessions.TryGetValue(sessionId, out var handle)) {
			WriteError(errorOut, InvalidSessionError);
			return null;
		}
		try {
			return AllocUtf8(handle.Session.GetResult(FromUtf8(partition)));
		} catch (Exception ex) {
			WriteError(errorOut, ex);
			return null;
		}
	}

	[UnmanagedCallersOnly(EntryPoint = "gaffer_session_get_sources")]
	public static byte* GetSources(nint sessionId, byte** errorOut) {
		if (errorOut != null)
			*errorOut = null;
		if (!Sessions.TryGetValue(sessionId, out var handle)) {
			WriteError(errorOut, InvalidSessionError);
			return null;
		}
		try {
			var info = ProjectionInfoMapper.ToProjectionInfo(handle.Session.Sources, handle.Session.Diagnostics, handle.Session.Shape);
			return AllocUtf8(JsonSerializer.Serialize(info, SdkJsonContext.Default.ProjectionInfo));
		} catch (Exception ex) {
			WriteError(errorOut, ex);
			return null;
		}
	}

	[UnmanagedCallersOnly(EntryPoint = "gaffer_session_get_partition_key")]
	public static byte* GetPartitionKey(nint sessionId, byte* eventJson, byte** errorOut) {
		if (errorOut != null)
			*errorOut = null;
		if (!Sessions.TryGetValue(sessionId, out var handle)) {
			WriteError(errorOut, InvalidSessionError);
			return null;
		}
		try {
			var json = FromUtf8(eventJson);
			if (json == null) {
				WriteError(errorOut, new InvalidArgumentException("event_json is null", "event_json"));
				return null;
			}
			var evt = ParseEvent(json);
			return AllocUtf8(handle.Session.GetPartitionKey(evt));
		} catch (Exception ex) {
			WriteError(errorOut, ex);
			return null;
		}
	}

	// -- Debug controls --

	[UnmanagedCallersOnly(EntryPoint = "gaffer_debug_set_breakpoint")]
	public static byte* DebugSetBreakpoint(nint sessionId, int line, int column, byte* condition, byte* hitCondition, byte* logMessage, byte** errorOut) {
		if (errorOut != null)
			*errorOut = null;
		if (!Sessions.TryGetValue(sessionId, out var handle)) {
			WriteError(errorOut, InvalidSessionError);
			return null;
		}
		try {
			var snapped = handle.Session.SetBreakpoint(line, column, FromUtf8(condition), FromUtf8(hitCondition), FromUtf8(logMessage));
			if (snapped == null)
				return null;
			return AllocUtf8($"{{\"line\":{snapped.Value.Line},\"column\":{snapped.Value.Column}}}");
		} catch (Exception ex) {
			WriteError(errorOut, ex);
			return null;
		}
	}

	[UnmanagedCallersOnly(EntryPoint = "gaffer_debug_clear_breakpoints")]
	public static void DebugClearBreakpoints(nint sessionId, byte** errorOut) {
		if (errorOut != null)
			*errorOut = null;
		if (!Sessions.TryGetValue(sessionId, out var handle)) {
			WriteError(errorOut, InvalidSessionError);
			return;
		}
		try {
			handle.Session.ClearBreakpoints();
		} catch (Exception ex) {
			WriteError(errorOut, ex);
		}
	}

	[UnmanagedCallersOnly(EntryPoint = "gaffer_debug_continue")]
	public static void DebugContinue(nint sessionId, byte** errorOut) {
		if (errorOut != null)
			*errorOut = null;
		if (!Sessions.TryGetValue(sessionId, out var handle)) {
			WriteError(errorOut, InvalidSessionError);
			return;
		}
		try {
			handle.Session.Continue();
		} catch (Exception ex) {
			WriteError(errorOut, ex);
		}
	}

	[UnmanagedCallersOnly(EntryPoint = "gaffer_debug_pause")]
	public static void DebugPause(nint sessionId, byte** errorOut) {
		if (errorOut != null)
			*errorOut = null;
		if (!Sessions.TryGetValue(sessionId, out var handle)) {
			WriteError(errorOut, InvalidSessionError);
			return;
		}
		try {
			handle.Session.Pause();
		} catch (Exception ex) {
			WriteError(errorOut, ex);
		}
	}

	[UnmanagedCallersOnly(EntryPoint = "gaffer_debug_step_into")]
	public static void DebugStepInto(nint sessionId, byte** errorOut) {
		if (errorOut != null)
			*errorOut = null;
		if (!Sessions.TryGetValue(sessionId, out var handle)) {
			WriteError(errorOut, InvalidSessionError);
			return;
		}
		try {
			handle.Session.StepInto();
		} catch (Exception ex) {
			WriteError(errorOut, ex);
		}
	}

	[UnmanagedCallersOnly(EntryPoint = "gaffer_debug_step_over")]
	public static void DebugStepOver(nint sessionId, byte** errorOut) {
		if (errorOut != null)
			*errorOut = null;
		if (!Sessions.TryGetValue(sessionId, out var handle)) {
			WriteError(errorOut, InvalidSessionError);
			return;
		}
		try {
			handle.Session.StepOver();
		} catch (Exception ex) {
			WriteError(errorOut, ex);
		}
	}

	[UnmanagedCallersOnly(EntryPoint = "gaffer_debug_step_out")]
	public static void DebugStepOut(nint sessionId, byte** errorOut) {
		if (errorOut != null)
			*errorOut = null;
		if (!Sessions.TryGetValue(sessionId, out var handle)) {
			WriteError(errorOut, InvalidSessionError);
			return;
		}
		try {
			handle.Session.StepOut();
		} catch (Exception ex) {
			WriteError(errorOut, ex);
		}
	}

	[UnmanagedCallersOnly(EntryPoint = "gaffer_debug_get_call_stack")]
	public static byte* DebugGetCallStack(nint sessionId, byte** errorOut) {
		if (errorOut != null)
			*errorOut = null;
		if (!Sessions.TryGetValue(sessionId, out var handle)) {
			WriteError(errorOut, InvalidSessionError);
			return null;
		}
		try {
			return AllocUtf8(SerializeCallStack(handle.Session.GetCallStack()));
		} catch (Exception ex) {
			WriteError(errorOut, ex);
			return null;
		}
	}

	[UnmanagedCallersOnly(EntryPoint = "gaffer_debug_get_scopes")]
	public static byte* DebugGetScopes(nint sessionId, int frameIndex, byte** errorOut) {
		if (errorOut != null)
			*errorOut = null;
		if (!Sessions.TryGetValue(sessionId, out var handle)) {
			WriteError(errorOut, InvalidSessionError);
			return null;
		}
		try {
			return AllocUtf8(SerializeScopes(handle.Session.GetScopes(frameIndex)));
		} catch (Exception ex) {
			WriteError(errorOut, ex);
			return null;
		}
	}

	[UnmanagedCallersOnly(EntryPoint = "gaffer_debug_get_variables")]
	public static byte* DebugGetVariables(nint sessionId, int variablesReference, byte** errorOut) {
		if (errorOut != null)
			*errorOut = null;
		if (!Sessions.TryGetValue(sessionId, out var handle)) {
			WriteError(errorOut, InvalidSessionError);
			return null;
		}
		try {
			return AllocUtf8(SerializeVariables(handle.Session.GetVariables(variablesReference)));
		} catch (Exception ex) {
			WriteError(errorOut, ex);
			return null;
		}
	}

	private static string SerializeCallStack(Events.DebugCallFrame[] frames) {
		using var stream = new System.IO.MemoryStream();
		using var writer = new Utf8JsonWriter(stream);
		writer.WriteStartArray();
		for (int i = 0; i < frames.Length; i++) {
			var f = frames[i];
			writer.WriteStartObject();
			writer.WriteNumber("id", f.Id);
			writer.WriteString("name", f.Name);
			writer.WriteNumber("line", f.Line);
			writer.WriteNumber("column", f.Column);
			writer.WriteEndObject();
		}
		writer.WriteEndArray();
		writer.Flush();
		return Encoding.UTF8.GetString(stream.ToArray());
	}

	private static string SerializeScopes(Events.DebugScopeInfo[] scopes) {
		using var stream = new System.IO.MemoryStream();
		using var writer = new Utf8JsonWriter(stream);
		writer.WriteStartArray();
		for (int i = 0; i < scopes.Length; i++) {
			var s = scopes[i];
			writer.WriteStartObject();
			writer.WriteString("name", s.Name);
			writer.WriteNumber("variablesReference", s.VariablesReference);
			writer.WriteBoolean("expensive", s.Expensive);
			writer.WriteEndObject();
		}
		writer.WriteEndArray();
		writer.Flush();
		return Encoding.UTF8.GetString(stream.ToArray());
	}

	private static string SerializeVariables(Events.DebugVariable[] variables) {
		using var stream = new System.IO.MemoryStream();
		using var writer = new Utf8JsonWriter(stream);
		writer.WriteStartArray();
		for (int i = 0; i < variables.Length; i++) {
			var v = variables[i];
			writer.WriteStartObject();
			writer.WriteString("name", v.Name);
			writer.WriteString("value", v.Value);
			writer.WriteString("type", v.Type);
			writer.WriteNumber("variablesReference", v.VariablesReference);
			writer.WriteEndObject();
		}
		writer.WriteEndArray();
		writer.Flush();
		return Encoding.UTF8.GetString(stream.ToArray());
	}

	[UnmanagedCallersOnly(EntryPoint = "gaffer_debug_evaluate")]
	public static byte* DebugEvaluate(nint sessionId, byte* expression, byte** errorOut) {
		if (errorOut != null)
			*errorOut = null;
		if (!Sessions.TryGetValue(sessionId, out var handle)) {
			WriteError(errorOut, InvalidSessionError);
			return null;
		}
		try {
			var expr = FromUtf8(expression);
			if (expr == null) {
				WriteError(errorOut, new InvalidArgumentException("expression is null", "expression"));
				return null;
			}
			return AllocUtf8(SerializeVariable(handle.Session.Evaluate(expr)));
		} catch (Exception ex) {
			WriteError(errorOut, ex);
			return null;
		}
	}

	private static string SerializeVariable(Events.DebugVariable v) {
		using var stream = new System.IO.MemoryStream();
		using var writer = new Utf8JsonWriter(stream);
		writer.WriteStartObject();
		writer.WriteString("name", v.Name);
		writer.WriteString("value", v.Value);
		writer.WriteString("type", v.Type);
		writer.WriteNumber("variablesReference", v.VariablesReference);
		writer.WriteEndObject();
		writer.Flush();
		return Encoding.UTF8.GetString(stream.ToArray());
	}

	// -- Memory management --

	[UnmanagedCallersOnly(EntryPoint = "gaffer_free")]
	public static void Free(void* ptr) {
		if (ptr != null)
			NativeMemory.Free(ptr);
	}

	// -- Helpers --

	internal static ProjectionSessionOptions ParseOptions(string? json) {
		if (string.IsNullOrEmpty(json))
			throw new InvalidArgumentException(
				"options are required. engineVersion must be set to 1 or 2.",
				"options");

		using var doc = JsonDocument.Parse(json);
		var root = doc.RootElement;
		return new ProjectionSessionOptions {
			EngineVersion = ParseEngineVersion(root),
			QuirksVersion = ParseQuirksVersion(root),
			CompilationTimeout = TimeSpan.FromMilliseconds(
				root.TryGetProperty("compilationTimeoutMs", out var ct) ? ct.GetInt32() : 5000),
			ExecutionTimeout = TimeSpan.FromMilliseconds(
				root.TryGetProperty("executionTimeoutMs", out var et) ? et.GetInt32() : 5000),
			Debug = root.TryGetProperty("debug", out var d) && d.GetBoolean(),
			IncludeShape = root.TryGetProperty("includeShape", out var inc) && inc.GetBoolean(),
		};
	}

	private static ProjectionVersion ParseEngineVersion(JsonElement root) {
		if (!root.TryGetProperty("engineVersion", out var v))
			throw new InvalidArgumentException(
				"engineVersion is required. Expected 1 or 2.",
				"engineVersion");
		return v.GetInt32() switch {
			1 => ProjectionVersion.V1,
			2 => ProjectionVersion.V2,
			var n => throw new InvalidArgumentException(
				$"Unknown engineVersion: {n}. Expected 1 or 2.",
				"engineVersion"),
		};
	}

	private static KurrentDbVersion? ParseQuirksVersion(JsonElement root) {
		if (!root.TryGetProperty("quirksVersion", out var v) || v.ValueKind == JsonValueKind.Null)
			return null;
		if (v.ValueKind != JsonValueKind.String)
			throw new InvalidArgumentException(
				"quirksVersion must be a string in MAJOR.MINOR.PATCH form (e.g. \"26.1.0\").",
				"quirksVersion");
		var s = v.GetString();
		if (string.IsNullOrEmpty(s))
			return null;
		if (!KurrentDbVersion.TryParse(s, out var version))
			throw new InvalidArgumentException(
				$"Invalid quirksVersion '{s}'. Expected MAJOR.MINOR.PATCH (e.g. 26.1.0).",
				"quirksVersion");
		return version;
	}

	internal static ProjectionEvent ParseEvent(string json) {
		try {
			using var doc = JsonDocument.Parse(json);
			var root = doc.RootElement;
			return new ProjectionEvent {
				EventType = root.GetProperty("eventType").GetString()!,
				StreamId = root.GetProperty("streamId").GetString()!,
				Data = root.TryGetProperty("data", out var data) && data.ValueKind != JsonValueKind.Null ? data.ToString() : null,
				Metadata = root.TryGetProperty("metadata", out var meta) && meta.ValueKind != JsonValueKind.Null ? meta.ToString() : null,
				LinkMetadata = root.TryGetProperty("linkMetadata", out var lm) && lm.ValueKind != JsonValueKind.Null ? lm.ToString() : null,
				SequenceNumber = root.GetProperty("sequenceNumber").GetInt64(),
				IsJson = root.GetProperty("isJson").GetBoolean(),
				EventId = Guid.Parse(root.GetProperty("eventId").GetString()!),
				Created = root.GetProperty("created").GetDateTime(),
			};
		} catch (JsonException ex) {
			throw new InvalidArgumentException("Malformed event JSON", "event_json", ex);
		} catch (KeyNotFoundException ex) {
			throw new InvalidArgumentException(ex.Message, "event_json", ex);
		} catch (FormatException ex) {
			throw new InvalidArgumentException(ex.Message, "event_json", ex);
		} catch (InvalidOperationException ex) {
			throw new InvalidArgumentException(ex.Message, "event_json", ex);
		}
	}

	private static byte* AllocUtf8(string? value) {
		if (value == null)
			return null;
		var bytes = Encoding.UTF8.GetBytes(value);
		var ptr = (byte*)NativeMemory.Alloc((nuint)(bytes.Length + 1));
		bytes.CopyTo(new Span<byte>(ptr, bytes.Length));
		ptr[bytes.Length] = 0;
		return ptr;
	}

	private static void FreeUtf8(byte* ptr) {
		if (ptr != null)
			NativeMemory.Free(ptr);
	}
}
