using System;
using System.Runtime.InteropServices.JavaScript;
using Gaffer.Runtime;
using Gaffer.Runtime.Events;

Console.WriteLine("Gaffer wasm runtime loaded");

public partial class GafferWasm {
	private static ProjectionSession? _session;

	[JSExport]
	internal static void CreateSession(string source) {
		_session?.Dispose();
		_session = new ProjectionSession(source, new ProjectionSessionOptions {
			EngineVersion = ProjectionVersion.V2,
		});
	}

	[JSExport]
	internal static void Feed(string eventType, string streamId, string data) {
		if (_session is null)
			throw new InvalidOperationException("No session");
		_session.Feed(new ProjectionEvent {
			EventType = eventType,
			StreamId = streamId,
			Data = data,
		});
	}

	[JSExport]
	internal static string GetState() {
		if (_session is null)
			throw new InvalidOperationException("No session");
		return _session.GetState() ?? "";
	}
}
