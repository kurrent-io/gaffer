using System.Runtime.CompilerServices;
using System.Runtime.InteropServices;
using System.Text;
using System.Text.Json;
using Gaffer.Runtime.Events;

namespace Gaffer.Runtime;

/// <summary>
/// C API exports for the gaffer runtime. These methods are callable from
/// native code (Go via cgo, Node via N-API, etc.) when built with NativeAOT.
/// </summary>
internal static unsafe class NativeExports {
	private static readonly Dictionary<nint, SessionHandle> Sessions = new();
	private static nint _nextHandle = 1;

	private sealed class SessionHandle {
		public required ProjectionSession Session { get; init; }
		public string? LastError { get; set; }
		public string? LastReturnedString { get; set; }

		// Prevent GC of delegates while callbacks are registered
		public GCHandle EmitCbHandle;
		public GCHandle LogCbHandle;
		public GCHandle SlowHandlerCbHandle;
		public GCHandle StateChangedCbHandle;
	}

	private static byte* ToUnmanaged(SessionHandle handle, string? value) {
		if (value == null) {
			handle.LastReturnedString = null;
			return null;
		}
		handle.LastReturnedString = value;
		// Return pointer to pinned managed string bytes.
		// Valid until next call (LastReturnedString keeps it alive).
		// Caller must copy.
		var bytes = Encoding.UTF8.GetBytes(value);
		var ptr = (byte*)NativeMemory.Alloc((nuint)(bytes.Length + 1));
		bytes.CopyTo(new Span<byte>(ptr, bytes.Length));
		ptr[bytes.Length] = 0; // null terminator
		return ptr;
	}

	private static string? FromUtf8(byte* ptr) {
		if (ptr == null) return null;
		return Marshal.PtrToStringUTF8((nint)ptr);
	}

	// -- Session lifecycle --

	[UnmanagedCallersOnly(EntryPoint = "gaffer_session_create")]
	public static nint Create(byte* source, byte* optionsJson) {
		try {
			var sourceStr = FromUtf8(source);
			if (sourceStr == null) return 0;

			var opts = ParseOptions(FromUtf8(optionsJson));
			var session = new ProjectionSession(sourceStr, opts);

			var handle = new SessionHandle { Session = session };
			var id = _nextHandle++;
			Sessions[id] = handle;
			return id;
		} catch (Exception ex) {
			// Can't return error through handle that doesn't exist yet.
			// Return 0 (null handle).
			_ = ex;
			return 0;
		}
	}

	[UnmanagedCallersOnly(EntryPoint = "gaffer_session_destroy")]
	public static void Destroy(nint sessionId) {
		if (!Sessions.TryGetValue(sessionId, out var handle)) return;

		if (handle.EmitCbHandle.IsAllocated) handle.EmitCbHandle.Free();
		if (handle.LogCbHandle.IsAllocated) handle.LogCbHandle.Free();
		if (handle.SlowHandlerCbHandle.IsAllocated) handle.SlowHandlerCbHandle.Free();
		if (handle.StateChangedCbHandle.IsAllocated) handle.StateChangedCbHandle.Free();

		handle.Session.Dispose();
		Sessions.Remove(sessionId);
	}

	// -- Callbacks --

	[UnmanagedCallersOnly(EntryPoint = "gaffer_on_emit")]
	public static void OnEmit(nint sessionId, delegate* unmanaged<byte*, byte*, byte*, byte*, void*, void> cb, void* userData) {
		if (!Sessions.TryGetValue(sessionId, out var handle)) return;

		handle.Session.OnEmit = emitted => {
			var streamId = AllocUtf8(emitted.StreamId);
			var eventType = AllocUtf8(emitted.EventType);
			var data = AllocUtf8(emitted.Data);
			var metadata = AllocUtf8(emitted.Metadata != null ? JsonSerializer.Serialize(emitted.Metadata, GafferJsonContext.Default.DictionaryStringString) : null);
			try {
				cb(streamId, eventType, data, metadata, userData);
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
		if (!Sessions.TryGetValue(sessionId, out var handle)) return;

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
		if (!Sessions.TryGetValue(sessionId, out var handle)) return;

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
		if (!Sessions.TryGetValue(sessionId, out var handle)) return;

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
	public static int Feed(nint sessionId, byte* eventJson) {
		if (!Sessions.TryGetValue(sessionId, out var handle)) return -1;

		try {
			var json = FromUtf8(eventJson);
			if (json == null) {
				handle.LastError = "event_json is null";
				return -1;
			}

			var evt = ParseEvent(json);
			handle.Session.Feed(evt);
			handle.LastError = null;
			return 0;
		} catch (Exception ex) {
			handle.LastError = ex.Message;
			return -1;
		}
	}

	// -- State --

	[UnmanagedCallersOnly(EntryPoint = "gaffer_session_get_state")]
	public static byte* GetState(nint sessionId, byte* partition) {
		if (!Sessions.TryGetValue(sessionId, out var handle)) return null;
		var state = handle.Session.GetState(FromUtf8(partition));
		return ToUnmanaged(handle, state);
	}

	[UnmanagedCallersOnly(EntryPoint = "gaffer_session_get_shared_state")]
	public static byte* GetSharedState(nint sessionId) {
		if (!Sessions.TryGetValue(sessionId, out var handle)) return null;
		return ToUnmanaged(handle, handle.Session.GetSharedState());
	}

	[UnmanagedCallersOnly(EntryPoint = "gaffer_session_set_state")]
	public static void SetState(nint sessionId, byte* partition, byte* stateJson) {
		if (!Sessions.TryGetValue(sessionId, out var handle)) return;
		var json = FromUtf8(stateJson);
		if (json != null)
			handle.Session.SetState(FromUtf8(partition), json);
	}

	[UnmanagedCallersOnly(EntryPoint = "gaffer_session_get_result")]
	public static byte* GetResult(nint sessionId, byte* partition) {
		if (!Sessions.TryGetValue(sessionId, out var handle)) return null;
		return ToUnmanaged(handle, handle.Session.GetResult(FromUtf8(partition)));
	}

	[UnmanagedCallersOnly(EntryPoint = "gaffer_session_get_sources")]
	public static byte* GetSources(nint sessionId) {
		if (!Sessions.TryGetValue(sessionId, out var handle)) return null;
		var sources = handle.Session.Sources;
		var json = JsonSerializer.Serialize(sources, GafferJsonContext.Default.QuerySources);
		return ToUnmanaged(handle, json);
	}

	[UnmanagedCallersOnly(EntryPoint = "gaffer_session_get_partition_key")]
	public static byte* GetPartitionKey(nint sessionId, byte* eventJson) {
		if (!Sessions.TryGetValue(sessionId, out var handle)) return null;
		try {
			var json = FromUtf8(eventJson);
			if (json == null) return null;
			var evt = ParseEvent(json);
			return ToUnmanaged(handle, handle.Session.GetPartitionKey(evt));
		} catch {
			return null;
		}
	}

	// -- Error handling --

	[UnmanagedCallersOnly(EntryPoint = "gaffer_session_get_error")]
	public static byte* GetError(nint sessionId) {
		if (!Sessions.TryGetValue(sessionId, out var handle)) return null;
		return ToUnmanaged(handle, handle.LastError);
	}

	// -- Helpers --

	private static ProjectionSessionOptions ParseOptions(string? json) {
		if (string.IsNullOrEmpty(json)) return new ProjectionSessionOptions();

		using var doc = JsonDocument.Parse(json);
		var root = doc.RootElement;
		return new ProjectionSessionOptions {
			HandlerTimeoutMs = root.TryGetProperty("handlerTimeoutMs", out var ht) ? ht.GetInt32() : 250,
			CompilationTimeout = TimeSpan.FromMilliseconds(
				root.TryGetProperty("compilationTimeoutMs", out var ct) ? ct.GetInt32() : 5000),
			ExecutionTimeout = TimeSpan.FromMilliseconds(
				root.TryGetProperty("executionTimeoutMs", out var et) ? et.GetInt32() : 5000),
			EnableContentTypeValidation = root.TryGetProperty("enableContentTypeValidation", out var cv) && cv.GetBoolean(),
			Debug = root.TryGetProperty("debug", out var d) && d.GetBoolean(),
		};
	}

	private static ProjectionEvent ParseEvent(string json) {
		using var doc = JsonDocument.Parse(json);
		var root = doc.RootElement;
		return new ProjectionEvent {
			EventType = root.GetProperty("eventType").GetString()!,
			StreamId = root.GetProperty("streamId").GetString()!,
			Data = root.TryGetProperty("data", out var data) ? data.ToString() : null,
			Metadata = root.TryGetProperty("metadata", out var meta) ? meta.ToString() : null,
			LinkMetadata = root.TryGetProperty("linkMetadata", out var lm) ? lm.ToString() : null,
			SequenceNumber = root.TryGetProperty("sequenceNumber", out var sn) ? sn.GetInt64() : 0,
			IsJson = !root.TryGetProperty("isJson", out var ij) || ij.GetBoolean(),
			EventId = root.TryGetProperty("eventId", out var eid) ? Guid.Parse(eid.GetString()!) : Guid.NewGuid(),
			Timestamp = root.TryGetProperty("timestamp", out var ts) ? ts.GetDateTime() : DateTime.UtcNow,
		};
	}

	private static byte* AllocUtf8(string? value) {
		if (value == null) return null;
		var bytes = Encoding.UTF8.GetBytes(value);
		var ptr = (byte*)NativeMemory.Alloc((nuint)(bytes.Length + 1));
		bytes.CopyTo(new Span<byte>(ptr, bytes.Length));
		ptr[bytes.Length] = 0;
		return ptr;
	}

	private static void FreeUtf8(byte* ptr) {
		if (ptr != null) NativeMemory.Free(ptr);
	}
}
