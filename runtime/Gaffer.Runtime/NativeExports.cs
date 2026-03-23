using System.Collections.Concurrent;
using System.Runtime.CompilerServices;
using System.Runtime.InteropServices;
using System.Text;
using System.Text.Json;
using Gaffer.Runtime.Errors;
using Gaffer.Runtime.Events;

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
		public byte* LastReturnedPtr { get; set; }

		// Prevent GC of delegates while callbacks are registered
		public GCHandle EmitCbHandle;
		public GCHandle LogCbHandle;
		public GCHandle SlowHandlerCbHandle;
		public GCHandle StateChangedCbHandle;
	}

	private static byte* ToUnmanaged(SessionHandle handle, string? value) {
		if (handle.LastReturnedPtr != null) {
			NativeMemory.Free(handle.LastReturnedPtr);
			handle.LastReturnedPtr = null;
		}

		if (value == null)
			return null;

		var bytes = Encoding.UTF8.GetBytes(value);
		var ptr = (byte*)NativeMemory.Alloc((nuint)(bytes.Length + 1));
		bytes.CopyTo(new Span<byte>(ptr, bytes.Length));
		ptr[bytes.Length] = 0;
		handle.LastReturnedPtr = ptr;
		return ptr;
	}

	private static string? FromUtf8(byte* ptr) {
		if (ptr == null)
			return null;
		return Marshal.PtrToStringUTF8((nint)ptr);
	}

	private static void WriteError(Exception ex, byte* errorBuf, int errorBufSize) {
		if (errorBuf == null || errorBufSize <= 0)
			return;

		string json;
		if (ex is GafferException ge)
			json = SerializeGafferError(ge);
		else
			json = SerializeUnexpectedError(ex);

		var bytes = Encoding.UTF8.GetBytes(json);
		var len = Math.Min(bytes.Length, errorBufSize - 1);
		bytes.AsSpan(0, len).CopyTo(new Span<byte>(errorBuf, len));
		errorBuf[len] = 0;
	}

	private static string SerializeGafferError(GafferException ex) {
		using var stream = new System.IO.MemoryStream();
		using var writer = new Utf8JsonWriter(stream);
		writer.WriteStartObject();
		writer.WriteString("code", ex.Code);
		writer.WriteString("description", ex.Description);

		switch (ex) {
			case InvalidProjectionException ip:
				if (ip.Index != null)
					writer.WriteNumber("index", ip.Index.Value);
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
		writer.WriteEndObject();
		writer.Flush();
		return Encoding.UTF8.GetString(stream.ToArray());
	}

	// -- Session lifecycle --

	[UnmanagedCallersOnly(EntryPoint = "gaffer_session_create")]
	public static nint Create(byte* source, byte* optionsJson, byte* errorBuf, int errorBufSize) {
		try {
			var sourceStr = FromUtf8(source);
			if (sourceStr == null) {
				WriteError(new InvalidArgumentException("source is null", "source"), errorBuf, errorBufSize);
				return 0;
			}

			var opts = ParseOptions(FromUtf8(optionsJson));
			var session = new ProjectionSession(sourceStr, opts);

			var handle = new SessionHandle { Session = session };
			var id = (nint)Interlocked.Increment(ref _nextHandle);
			Sessions[id] = handle;
			return id;
		} catch (Exception ex) {
			WriteError(ex, errorBuf, errorBufSize);
			return 0;
		}
	}

	[UnmanagedCallersOnly(EntryPoint = "gaffer_session_destroy")]
	public static void Destroy(nint sessionId) {
		if (!Sessions.TryRemove(sessionId, out var handle))
			return;

		try {
			if (handle.LastReturnedPtr != null)
				NativeMemory.Free(handle.LastReturnedPtr);

			if (handle.EmitCbHandle.IsAllocated)
				handle.EmitCbHandle.Free();
			if (handle.LogCbHandle.IsAllocated)
				handle.LogCbHandle.Free();
			if (handle.SlowHandlerCbHandle.IsAllocated)
				handle.SlowHandlerCbHandle.Free();
			if (handle.StateChangedCbHandle.IsAllocated)
				handle.StateChangedCbHandle.Free();

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

	[UnmanagedCallersOnly(EntryPoint = "gaffer_on_slow_handler")]
	public static void OnSlowHandler(nint sessionId, delegate* unmanaged<byte*, int, void*, void> cb, void* userData) {
		if (!Sessions.TryGetValue(sessionId, out var handle))
			return;

		handle.Session.OnSlowHandler = (handlerName, durationMs) => {
			var name = AllocUtf8(handlerName);
			try {
				cb(name, durationMs, userData);
			} finally {
				FreeUtf8(name);
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

	// -- Event feeding --

	[UnmanagedCallersOnly(EntryPoint = "gaffer_session_feed")]
	public static int Feed(nint sessionId, byte* eventJson, byte* errorBuf, int errorBufSize) {
		if (!Sessions.TryGetValue(sessionId, out var handle))
			return -1;

		try {
			var json = FromUtf8(eventJson);
			if (json == null) {
				WriteError(new InvalidArgumentException("event_json is null", "event_json"), errorBuf, errorBufSize);
				return -1;
			}

			var evt = ParseEvent(json);
			handle.Session.Feed(evt);
			return 0;
		} catch (Exception ex) {
			WriteError(ex, errorBuf, errorBufSize);
			return -1;
		}
	}

	// -- State --

	[UnmanagedCallersOnly(EntryPoint = "gaffer_session_get_state")]
	public static byte* GetState(nint sessionId, byte* partition, byte* errorBuf, int errorBufSize) {
		if (!Sessions.TryGetValue(sessionId, out var handle))
			return null;
		try {
			var state = handle.Session.GetState(FromUtf8(partition));
			return ToUnmanaged(handle, state);
		} catch (Exception ex) {
			WriteError(ex, errorBuf, errorBufSize);
			return null;
		}
	}

	[UnmanagedCallersOnly(EntryPoint = "gaffer_session_get_shared_state")]
	public static byte* GetSharedState(nint sessionId, byte* errorBuf, int errorBufSize) {
		if (!Sessions.TryGetValue(sessionId, out var handle))
			return null;
		try {
			return ToUnmanaged(handle, handle.Session.GetSharedState());
		} catch (Exception ex) {
			WriteError(ex, errorBuf, errorBufSize);
			return null;
		}
	}

	[UnmanagedCallersOnly(EntryPoint = "gaffer_session_set_state")]
	public static void SetState(nint sessionId, byte* partition, byte* stateJson) {
		if (!Sessions.TryGetValue(sessionId, out var handle))
			return;
		try {
			var json = FromUtf8(stateJson);
			if (json != null)
				handle.Session.SetState(FromUtf8(partition), json);
		} catch {
			// SetState cannot meaningfully fail with our error types
		}
	}

	[UnmanagedCallersOnly(EntryPoint = "gaffer_session_get_result")]
	public static byte* GetResult(nint sessionId, byte* partition, byte* errorBuf, int errorBufSize) {
		if (!Sessions.TryGetValue(sessionId, out var handle))
			return null;
		try {
			return ToUnmanaged(handle, handle.Session.GetResult(FromUtf8(partition)));
		} catch (Exception ex) {
			WriteError(ex, errorBuf, errorBufSize);
			return null;
		}
	}

	[UnmanagedCallersOnly(EntryPoint = "gaffer_session_get_sources")]
	public static byte* GetSources(nint sessionId) {
		if (!Sessions.TryGetValue(sessionId, out var handle))
			return null;
		try {
			var sources = handle.Session.Sources;
			var json = JsonSerializer.Serialize(sources, GafferJsonContext.Default.QuerySources);
			return ToUnmanaged(handle, json);
		} catch {
			return null;
		}
	}

	[UnmanagedCallersOnly(EntryPoint = "gaffer_session_get_partition_key")]
	public static byte* GetPartitionKey(nint sessionId, byte* eventJson, byte* errorBuf, int errorBufSize) {
		if (!Sessions.TryGetValue(sessionId, out var handle))
			return null;
		try {
			var json = FromUtf8(eventJson);
			if (json == null) {
				WriteError(new InvalidArgumentException("event_json is null", "event_json"), errorBuf, errorBufSize);
				return null;
			}
			var evt = ParseEvent(json);
			return ToUnmanaged(handle, handle.Session.GetPartitionKey(evt));
		} catch (Exception ex) {
			WriteError(ex, errorBuf, errorBufSize);
			return null;
		}
	}

	// -- Helpers --

	private static ProjectionSessionOptions ParseOptions(string? json) {
		if (string.IsNullOrEmpty(json))
			return new ProjectionSessionOptions();

		using var doc = JsonDocument.Parse(json);
		var root = doc.RootElement;
		return new ProjectionSessionOptions {
			Version = ParseVersion(root),
			HandlerTimeoutMs = root.TryGetProperty("handlerTimeoutMs", out var ht) ? ht.GetInt32() : 250,
			CompilationTimeout = TimeSpan.FromMilliseconds(
				root.TryGetProperty("compilationTimeoutMs", out var ct) ? ct.GetInt32() : 5000),
			ExecutionTimeout = TimeSpan.FromMilliseconds(
				root.TryGetProperty("executionTimeoutMs", out var et) ? et.GetInt32() : 5000),
			EnableContentTypeValidation = root.TryGetProperty("enableContentTypeValidation", out var cv) && cv.GetBoolean(),
			Debug = root.TryGetProperty("debug", out var d) && d.GetBoolean(),
		};
	}

	private static ProjectionVersion ParseVersion(JsonElement root) {
		if (!root.TryGetProperty("version", out var v))
			return ProjectionVersion.V2;
		return v.GetString() switch {
			"v1" => ProjectionVersion.V1,
			"v2" => ProjectionVersion.V2,
			_ => throw new InvalidArgumentException(
				$"Unknown projection version: \"{v.GetString()}\". Expected \"v1\" or \"v2\".",
				"version"),
		};
	}

	private static ProjectionEvent ParseEvent(string json) {
		try {
			using var doc = JsonDocument.Parse(json);
			var root = doc.RootElement;
			return new ProjectionEvent {
				EventType = root.GetProperty("eventType").GetString()!,
				StreamId = root.GetProperty("streamId").GetString()!,
				Data = root.TryGetProperty("data", out var data) ? data.ToString() : null,
				Metadata = root.TryGetProperty("metadata", out var meta) ? meta.ToString() : null,
				LinkMetadata = root.TryGetProperty("linkMetadata", out var lm) ? lm.ToString() : null,
				SequenceNumber = root.GetProperty("sequenceNumber").GetInt64(),
				IsJson = root.GetProperty("isJson").GetBoolean(),
				EventId = Guid.Parse(root.GetProperty("eventId").GetString()!),
				Timestamp = root.GetProperty("timestamp").GetDateTime(),
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
