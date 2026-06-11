using System.Buffers;
using System.Collections;
using System.Collections.Concurrent;
using System.Diagnostics;
using System.Globalization;
using System.Runtime.CompilerServices;
using System.Text;
using System.Text.Encodings.Web;
using System.Text.Json;
using Gaffer.Runtime.Events;
using Gaffer.Runtime.Projection.Diagnostics.Behaviours;
using Gaffer.Sdk.Diagnostics;
using Gaffer.Sdk.Versioning;
using Jint;
using Jint.Native;
using Jint.Native.Function;
using Jint.Native.Json;
using Jint.Native.Object;
using Jint.Runtime;
using Jint.Runtime.Debugger;
using Jint.Runtime.Descriptors;
using Jint.Runtime.Interop;

namespace Gaffer.Runtime.Projection;

internal sealed class JintProjectionHandler : IDisposable {
	private static readonly Stopwatch Sw = Stopwatch.StartNew();

	// Sandbox guardrails against hostile or buggy projection code. Without these a
	// runaway script raises an uncatchable StackOverflowException (or OOMs the host)
	// before the time constraint can fire.
	//
	// Jint's StackGuard (MaxExecutionStackCount) is the recursion guard: it probes real
	// native stack headroom via RuntimeHelpers.TryEnsureSufficientExecutionStack and, when
	// the stack is running low, raises a catchable RangeError ("Maximum call stack size
	// exceeded") once the logical call depth exceeds this count - the same error a browser
	// engine produces. We deliberately do NOT use Jint's LimitRecursion: empirically it
	// fails to prevent the crash and itself overflows the host when the per-call-site limit
	// is reached (verified against Jint 4.6.3). See reference_jint_resource_limits.
	//
	// This count is the throw-vs-spill threshold at the native-stack-low point, NOT a cap on
	// legitimate recursion - that is bounded by actual native stack capacity (thousands of
	// frames). It must stay well below the smallest native-stack-low depth across hosts to
	// throw cleanly instead of spilling execution onto fresh threads: spilling both amplified
	// the original crash and breaks the per-thread memory accounting below. This assumes the
	// engine runs on a >=1MB-stack thread (bottoms out around ~1000 frames); the CLI, LSP and
	// FFI hosts all invoke on default-sized OS threads that satisfy this.
	private const int MaxExecutionStackCount = 200;

	// Bounds cumulative allocation per handler invocation (reset before each call via
	// _engine.Constraints.Reset()). Checked at statement boundaries, so it catches
	// iterative/accumulating allocation; a single huge native allocation completes before
	// the check fires and trips it on the next statement. 256 MiB is a deliberately generous
	// ceiling: real handlers allocate KB-MB per event, so it only trips on runaway growth.
	private const long MaxAllocatedBytesPerOperation = 256L * 1024 * 1024;

	private readonly Engine _engine;
	private readonly SourceDefinitionBuilder _definitionBuilder;
	private readonly ProjectionRuntime _runtime;
	private readonly JsonParser _parser;
	private readonly Serializer _serializer;
	private readonly Action<string>? _onLog;
	private readonly Action<EmittedEvent>? _onEmit;
	private readonly Action<Diagnostic>? _onDiagnostic;
	// Capabilities the co-located runtime quirk behaviours borrow; built once below.
	private readonly QuirkContext _quirkContext;
	// KurrentDB enables content type validation for all projections on subsystem version >= 4.
	// Editing a projection auto-bumps the subsystem version, so all active projections get this
	// behavior. We match that by always enabling it rather than exposing a toggle.
	private readonly bool _enableContentTypeValidation = true;
	private readonly bool _debug;
	private readonly KurrentDbVersion? _quirksVersion;
	private readonly ProjectionVersion _engineVersion;
	private readonly TimeConstraint _timeConstraint;
	private readonly BlockingCollection<DebugCommand> _debugCommands = new();
	private readonly Dictionary<int, object> _variableStore = new();
	private List<BreakablePosition>? _breakablePositions;
	private int _nextVariableRef = 1;
	private DebugInformation? _currentDebugInfo;
	private volatile bool _paused;
	private volatile bool _pauseRequested;
	private bool _evaluating;

	private JsValue _state;
	private JsValue _sharedState;
	private bool _faulted;

	/// <summary>
	/// Key under which quirk-firing branches stash a <c>DiagnosticCatalog</c> quirk code on
	/// <see cref="Exception.Data"/> so <see cref="ProjectionSession"/> can
	/// pluck it off when wrapping the exception.
	/// </summary>
	internal const string CompatCodeDataKey = "GafferCompatCode";

	/// <summary>
	/// Run an upstream-quirk-firing code block whose throw will be wrapped by
	/// Jint into a <c>JavaScriptException</c>. On throw, stash the quirk's
	/// code on <see cref="Exception.Data"/> so
	/// <see cref="ProjectionSession.WrapHandlerException"/> can plumb it
	/// through to <see cref="ProjectionException.CompatCode"/>.
	/// <para>
	/// Use this only for the Jint-wrapped path - i.e. when our CLR code
	/// throws inside a method Jint calls and the runtime sees
	/// <c>JavaScriptException</c> with our throw as <c>InnerException</c>.
	/// If the quirk-firing branch throws a <see cref="ProjectionException"/>
	/// directly (e.g. <c>StateSerializationException</c> for NaN/Infinity),
	/// set <c>CompatCode</c> on the exception's object initializer instead.
	/// </para>
	/// </summary>
	private static T RunQuirkyPath<T>(DiagnosticDescriptor quirk, Func<T> body) {
		try {
			return body();
		} catch (Exception ex) {
			ex.Data[CompatCodeDataKey] = quirk.Code;
			throw;
		}
	}

	private static void RunQuirkyPath(DiagnosticDescriptor quirk, Action body) {
		try {
			body();
		} catch (Exception ex) {
			ex.Data[CompatCodeDataKey] = quirk.Code;
			throw;
		}
	}

	/// <summary>Fired when execution pauses at a breakpoint or debugger statement. Informational only.</summary>
	public Action<BreakInfo>? OnBreak { get; set; }

	public JintProjectionHandler(
		string source,
		TimeSpan compilationTimeout,
		TimeSpan executionTimeout,
		ProjectionVersion engineVersion,
		Action<string>? onLog = null,
		Action<EmittedEvent>? onEmit = null,
		Action<Diagnostic>? onDiagnostic = null,
		bool debug = false,
		KurrentDbVersion? quirksVersion = null) {
		_onLog = onLog;
		_onEmit = onEmit;
		_onDiagnostic = onDiagnostic;
		_debug = debug;
		_quirksVersion = quirksVersion;
		_engineVersion = engineVersion;
		_definitionBuilder = new SourceDefinitionBuilder();
		_definitionBuilder.NoWhen();
		_definitionBuilder.AllEvents();
		_timeConstraint = new TimeConstraint(compilationTimeout, executionTimeout);
		if (debug)
			_timeConstraint.Disabled = true;
		_engine = new Engine(opts => {
			opts.Constraint(_timeConstraint)
				.DisableStringCompilation()
				.LimitMemory(MaxAllocatedBytesPerOperation);
			opts.Constraints.MaxExecutionStackCount = MaxExecutionStackCount;
			if (debug) {
				opts.Debugger.Enabled = true;
				opts.Debugger.StatementHandling = DebuggerStatementHandling.Script;
			}
		});
		_state = JsValue.Undefined;
		_sharedState = JsValue.Undefined;
		_runtime = new ProjectionRuntime(_engine, _definitionBuilder);
		_serializer = new Serializer(_quirksVersion);
		_quirkContext = new QuirkContext {
			OnDiagnostic = _onDiagnostic,
			ToPersistedString = ConvertToStringHandlingNulls,
			Serialize = Serialize,
			Log = SafeInvokeLog,
			AsString = AsString,
		};
		_engine.Global.FastAddProperty("log", new ClrFunction(_engine, "log", Log), false, false, false);

		if (debug) {
			_engine.Debugger.Break += OnDebugBreak;
			_engine.Debugger.Step += OnDebugStep;
			_engine.Debugger.BeforeEvaluate += OnBeforeEvaluate;
		}

		_timeConstraint.Compiling();
		_engine.Execute(source, "projection.js");
		_timeConstraint.Executing();
		_parser = _runtime.SwitchToExecutionMode();

		_engine.Global.FastAddProperty("emit", new ClrFunction(_engine, "emit", Emit, 4), true, false, true);
		_engine.Global.FastAddProperty("linkTo", new ClrFunction(_engine, "linkTo", LinkTo, 3), true, false, true);
		_engine.Global.FastAddProperty("linkStreamTo", new ClrFunction(_engine, "linkStreamTo", LinkStreamTo, 3), true, false, true);
		_engine.Global.FastAddProperty("copyTo", new ClrFunction(_engine, "copyTo", CopyTo, 3), true, false, true);
	}

	public void Dispose() {
		_debugCommands.CompleteAdding();
		_engine.Dispose();
	}

	private void EnsureNotFaulted() {
		if (_faulted)
			throw new InvalidOperationException("Session is faulted due to a callback error and cannot process further events");
	}

	private void SafeInvokeEmit(EmittedEvent emitted) {
		try {
			_onEmit?.Invoke(emitted);
		} catch (Exception) {
			_faulted = true;
			throw;
		}
	}

	private void SafeInvokeLog(string message) {
		try {
			_onLog?.Invoke(message);
		} catch (Exception) {
			_faulted = true;
			throw;
		}
	}

	public QuerySources GetSourceDefinition() {
		_engine.Constraints.Reset();
		return _definitionBuilder.Build();
	}

	public void Load(string? state) {
		_engine.Constraints.Reset();
		LoadCurrentState(state != null ? _parser.Parse(state) : JsValue.Null);
	}

	private void LoadCurrentState(JsValue jsValue) {
		if (_definitionBuilder.IsBiState) {
			if (_state == null || _state == JsValue.Undefined)
				_state = new JsArray(_engine, new[] { JsValue.Undefined, JsValue.Undefined });
			_state.AsArray()[0] = jsValue;
		} else {
			_state = jsValue;
		}
	}

	public void LoadShared(string? state) {
		_engine.Constraints.Reset();
		LoadCurrentSharedState(state != null ? _parser.Parse(state) : JsValue.Null);
	}

	private void LoadCurrentSharedState(JsValue jsValue) {
		if (_definitionBuilder.IsBiState) {
			if (_state == null || _state == JsValue.Undefined)
				_state = new JsArray(_engine, new[] { JsValue.Undefined, JsValue.Undefined });
			_state.AsArray()[1] = jsValue;
		} else {
			_state = jsValue;
		}
	}

	public void Initialize() {
		_engine.Constraints.Reset();
		LoadCurrentState(_runtime.InitializeState());
	}

	public void InitializeShared() {
		_engine.Constraints.Reset();
		_sharedState = _runtime.InitializeSharedState();
		LoadCurrentSharedState(_sharedState);
	}

	public string? GetStatePartition(ProjectionEvent @event, string category) {
		_engine.Constraints.Reset();
		var envelope = CreateEnvelope("", @event, category);
		var partition = _runtime.GetPartition(envelope);
		if (partition == JsValue.Null || partition == JsValue.Undefined || !(partition.IsString() || partition.IsNumber()))
			return null;
		return partition.IsNumber() ? partition.AsNumber().ToString(CultureInfo.InvariantCulture) : partition.AsString();
	}

	public bool ProcessEvent(
		string partition, string category, ProjectionEvent @event,
		out string? newState, out string? newSharedState) {
		EnsureNotFaulted();
		_engine.Constraints.Reset();
		if ((@event.IsJson && string.IsNullOrWhiteSpace(@event.Data)) ||
			(!_enableContentTypeValidation && !@event.IsJson && string.IsNullOrEmpty(@event.Data))) {
			PrepareOutput(out newState, out newSharedState, reportQuirks: true);
			return true;
		}

		var envelope = CreateEnvelope(partition, @event, category);
		_state = _runtime.Handle(_state, envelope);
		PrepareOutput(out newState, out newSharedState, reportQuirks: true);
		return true;
	}

	public void ProcessPartitionCreated(string partition, ProjectionEvent @event) {
		EnsureNotFaulted();
		_engine.Constraints.Reset();
		var envelope = CreateEnvelope(partition, @event, "");
		_runtime.HandleCreated(_state, envelope);
	}

	public bool ProcessPartitionDeleted(string partition, out string? newState) {
		EnsureNotFaulted();
		_engine.Constraints.Reset();
		_runtime.HandleDeleted(_state, partition, false);
		newState = ConvertToStringHandlingNulls(_state);
		return true;
	}

	public string? TransformStateToResult() {
		_engine.Constraints.Reset();
		if (_engineVersion == ProjectionVersion.V2) {
			// V2 doesn't apply transformBy/filterBy - state is the result.
			// JS calls to those functions still register the transforms
			// (matches upstream V2's silent acceptance), but the engine
			// never iterates them. Reuse PrepareOutput's conversion logic
			// (bi-state slots and uni-state string handling) so GetResult()
			// returns exactly what GetState() does under V2. See
			// cli/internal/mcpserver/resources/v1-v2-differences.md.
			PrepareOutput(out var newState, out _, reportQuirks: false);
			return newState;
		}
		var result = _runtime.TransformStateToResult(_state);
		if (result == JsValue.Null || result == JsValue.Undefined)
			return null;
		return Serialize(result);
	}

	// reportQuirks gates runtime-diagnostic emission. The V2 result-pass
	// (TransformStateToResult) reuses this purely to convert state to the
	// result and passes false, so a quirk isn't reported twice for one event;
	// the state-pass passes true.
	private void PrepareOutput(out string? newState, out string? newSharedState, bool reportQuirks) {
		if (_definitionBuilder.IsBiState && _state.IsArray()) {
			// Bi-state slots always JSON-encode, both slots (matches upstream's
			// ConvertToStringHandlingNulls). A string slot persists as `"hello"`, not raw -
			// that is the correct contract, not a quirk.
			var arr = _state.AsArray();
			newState = arr.TryGetValue(0, out var state) ? ConvertToStringHandlingNulls(state) : "";
			newSharedState = arr.TryGetValue(1, out var sharedState) ? ConvertToStringHandlingNulls(sharedState) : null;
		} else if (_state.IsString()) {
			newState = ConvertUniStateString(_state, reportQuirks);
			newSharedState = null;
		} else {
			newState = ConvertToStringHandlingNulls(_state);
			newSharedState = null;
		}
	}

	// The engine persists string state raw. Valid JSON text round-trips on reload (e.g. V1's
	// unhandled-event-replaces-with-body sets state to the raw body), so pass it through unchanged.
	// A bare non-JSON string faults on a reload's JsonParser.Parse pre-26.2.0
	// (quirk.serialize.rawString); gaffer can't carry raw invalid JSON on the wire, so it
	// JSON-encodes that case (safe) and reports the quirk when it fires.
	private string? ConvertUniStateString(JsValue state, bool reportQuirks) {
		var str = state.AsString();
		if (IsValidJson(str))
			return str;
		var report = reportQuirks && DiagnosticCatalog.SerializeRawString.FiresAt(_quirksVersion);
		return RawStringStateBehaviour.Apply(state, report, _quirkContext);
	}

	private static bool IsValidJson(string s) {
		try {
			using var _ = System.Text.Json.JsonDocument.Parse(s);
			return true;
		} catch (System.Text.Json.JsonException) {
			return false;
		}
	}

	private string? ConvertToStringHandlingNulls(JsValue value) =>
		value.IsNull() || value.IsUndefined() ? null : Serialize(value);

	private JsValue Emit(JsValue thisValue, JsValue[] parameters) {
		if (parameters.Length < 3)
			throw new ArgumentException("invalid number of parameters");

		var stream = EnsureNonNullStringValue(parameters.At(0), "streamId");
		var eventType = EnsureNonNullStringValue(parameters.At(1), "eventName");
		var eventBody = EnsureNonNullObjectValue(parameters.At(2), "eventBody");

		if (parameters.Length == 4 && !parameters.At(3).IsObject())
			throw new ArgumentException("object expected", "metadata");

		var data = Serialize(eventBody);
		Dictionary<string, string?>? metadata = null;
		if (parameters.Length == 4) {
			metadata = new Dictionary<string, string?>();
			foreach (var kvp in parameters.At(3).AsObject().GetOwnProperties()) {
				if (kvp.Value.Value.Type is Types.Empty or Types.Undefined)
					continue;
				metadata.Add(kvp.Key.AsString(), AsString(kvp.Value.Value, false));
			}
		}

		var emitted = new EmittedEvent {
			StreamId = stream,
			EventType = eventType,
			Data = data,
			IsJson = true,
			Metadata = metadata,
		};
		SafeInvokeEmit(emitted);
		return JsValue.Undefined;
	}

	private JsValue LinkTo(JsValue thisValue, JsValue[] parameters) {
		if (parameters.Length is not (2 or 3))
			throw new ArgumentException("wrong number of parameters");

		var stream = EnsureNonNullStringValue(parameters.At(0), "streamId");
		var @event = EnsureNonNullObjectValue(parameters.At(1), "event");

		var hasNumber = @event.TryGetValue("sequenceNumber", out var numberValue);
		var hasSource = @event.TryGetValue("streamId", out var sourceValue);
		if (!hasNumber || !hasSource || !numberValue.IsNumber() || !sourceValue.IsString())
			throw new Exception($"Invalid link to event {numberValue}@{sourceValue}");

		var number = (long)numberValue.AsNumber();
		var source = sourceValue.AsString();

		Dictionary<string, string?>? metadata = null;
		if (parameters.Length == 3) {
			metadata = new Dictionary<string, string?>();
			foreach (var kvp in EnsureNonNullObjectValue(parameters.At(2), "metaData").GetOwnProperties())
				metadata.Add(kvp.Key.AsString(), AsString(kvp.Value.Value, false));
		}

		var emitted = new EmittedEvent {
			StreamId = stream,
			EventType = "$>",
			Data = $"{number}@{source}",
			IsJson = false,
			Metadata = metadata,
		};
		SafeInvokeEmit(emitted);
		return JsValue.Undefined;
	}

	private JsValue LinkStreamTo(JsValue thisValue, JsValue[] parameters) {
		var stream = EnsureNonNullStringValue(parameters.At(0), "streamId");
		var linkedStreamId = EnsureNonNullStringValue(parameters.At(1), "linkedStreamId");

		Dictionary<string, string?>? metadata = null;
		if (parameters.Length == 3) {
			if (DiagnosticCatalog.LinkStreamToOutOfBoundsParameters.FiresAt(_quirksVersion)) {
				RunQuirkyPath(DiagnosticCatalog.LinkStreamToOutOfBoundsParameters,
					() => metadata = LinkStreamToMetadataBehaviour.Apply(parameters, _quirkContext));
			} else {
				metadata = new Dictionary<string, string?>();
				foreach (var kvp in parameters.At(2).AsObject().GetOwnProperties())
					metadata.Add(kvp.Key.AsString(), AsString(kvp.Value.Value, false));
			}
		}

		var emitted = new EmittedEvent {
			StreamId = stream,
			EventType = "$@",
			Data = linkedStreamId,
			IsJson = false,
			Metadata = metadata,
		};
		SafeInvokeEmit(emitted);
		return JsValue.Undefined;
	}

	private JsValue CopyTo(JsValue thisValue, JsValue[] parameters) => JsValue.Undefined;

	private JsValue Log(JsValue thisValue, JsValue[] parameters) {
		if (parameters.Length == 0 || _onLog == null)
			return JsValue.Undefined;

		if (parameters.Length == 1) {
			var p0 = parameters.At(0);
			if (p0 != null && p0.IsPrimitive())
				SafeInvokeLog(p0.ToString());
			else if (p0 is ObjectInstance oi)
				SafeInvokeLog(Serialize(oi));
			return JsValue.Undefined;
		}

		if (DiagnosticCatalog.LogMultiParam.FiresAt(_quirksVersion)) {
			LogMultiParamBehaviour.Apply(parameters, _quirkContext);
			return JsValue.Undefined;
		}

		var sb = new StringBuilder();
		for (var i = 0; i < parameters.Length; i++) {
			if (i > 0)
				sb.Append(", ");
			var p = parameters.At(i);
			if (p != null && p.IsPrimitive())
				sb.Append(p.ToString());
			else if (p is ObjectInstance oi)
				sb.Append(Serialize(oi));
		}
		SafeInvokeLog(sb.ToString());
		return JsValue.Undefined;
	}

	private static ObjectInstance EnsureNonNullObjectValue(JsValue parameter, string parameterName) {
		if (parameter == JsValue.Null || parameter == JsValue.Undefined)
			throw new ArgumentNullException(parameterName);
		if (!parameter.IsObject())
			throw new ArgumentException("object expected", parameterName);
		return parameter.AsObject();
	}

	private static string EnsureNonNullStringValue(JsValue parameter, string parameterName) {
		if (parameter != JsValue.Null && parameter.IsString() &&
			parameter.AsString() is { } value && !string.IsNullOrWhiteSpace(value))
			return value;
		if (parameter == JsValue.Null || parameter == JsValue.Undefined || parameter.IsString())
			throw new ArgumentNullException(parameterName);
		throw new ArgumentException("string expected", parameterName);
	}

	private string? AsString(JsValue? value, bool formatForRaw) => value switch {
		JsBoolean b => b.AsBoolean() ? "true" : "false",
		JsString s => formatForRaw ? $"\"{s.AsString()}\"" : s.AsString(),
		JsNumber n => n.AsNumber().ToString(CultureInfo.InvariantCulture),
		JsNull => null,
		JsUndefined => null,
		{ } v => Serialize(value),
		_ => null,
	};

	private EventEnvelope CreateEnvelope(string partition, ProjectionEvent @event, string category) {
		var envelope = new EventEnvelope(_engine, _parser, this);
		envelope.Partition = partition;
		envelope.Created = @event.Created;
		envelope.BodyRaw = @event.Data;
		envelope.MetadataRaw = @event.Metadata;
		envelope.StreamId = @event.StreamId;
		envelope.EventId = @event.EventId.ToString("D");
		envelope.EventType = @event.EventType;
		envelope.IsJson = @event.IsJson;
		envelope.LinkMetadataRaw = @event.LinkMetadata;
		envelope.Category = category;
		envelope.SequenceNumber = @event.SequenceNumber;
		return envelope;
	}

	internal string Serialize(JsValue value) =>
		Encoding.UTF8.GetString(_serializer.Serialize(value).Span);

	// -- Debug support --

	private StepMode OnDebugBreak(object sender, DebugInformation info) {
		if (_evaluating)
			return StepMode.None;

		if (info.BreakPoint is PauseBreakPoint) {
			RemovePauseBreakPoints();
			return EnterDebugCommandLoop("pause", info);
		}

		if (info.BreakPoint is GafferBreakPoint gbp) {
			gbp.HitCount++;

			if (gbp.HitConditionFunc != null && !gbp.HitConditionFunc(gbp.HitCount))
				return StepMode.None;

			if (gbp.LogMessage != null) {
				var message = EvaluateLogMessage(gbp.LogMessage);
				_onLog?.Invoke(message);
				return StepMode.None;
			}
		}

		var reason = info.PauseType == PauseType.DebuggerStatement ? "debugger_statement" : "breakpoint";
		return EnterDebugCommandLoop(reason, info);
	}

	private StepMode OnDebugStep(object sender, DebugInformation info) {
		if (_evaluating)
			return StepMode.None;
		return EnterDebugCommandLoop("step", info);
	}


	private void OnBeforeEvaluate(object sender, Acornima.Ast.Program ast) {
		var collector = new BreakablePositionCollector();
		collector.Visit(ast);
		_breakablePositions = collector.Positions;
		_breakablePositions.Sort((a, b) => a.Line != b.Line ? a.Line.CompareTo(b.Line) : a.Column.CompareTo(b.Column));
	}

	private StepMode EnterDebugCommandLoop(string reason, DebugInformation info) {
		_paused = true;
		_currentDebugInfo = info;
		_variableStore.Clear();
		_nextVariableRef = 1;
		try {
			var location = info.Location;
			OnBreak?.Invoke(new BreakInfo {
				Reason = reason,
				Line = location.Start.Line,
				Column = location.Start.Column + 1, // Jint 0-based -> 1-based
			});

			foreach (var cmd in _debugCommands.GetConsumingEnumerable()) {
				switch (cmd) {
					case ContinueCommand cc:
						ClearDebugState();
						cc.Done.Set();
						return StepMode.None;
					case StepCommand sc:
						ClearDebugState();
						sc.Done.Set();
						return sc.Mode;
					case GetCallStackCommand gc:
						try { gc.Result = ReadCallStack(info); } catch (Exception ex) { gc.Error = ex; }
						gc.Done.Set();
						break;
					case GetScopesCommand sc:
						try { sc.Result = ReadScopes(info, sc.FrameIndex); } catch (Exception ex) { sc.Error = ex; }
						sc.Done.Set();
						break;
					case GetVariablesCommand vc:
						try { vc.Result = ReadVariables(vc.VariablesReference); } catch (Exception ex) { vc.Error = ex; }
						vc.Done.Set();
						break;
					case EvaluateCommand ec:
						try { ec.Result = RunEvaluate(ec.Expression); } catch (Exception ex) { ec.Error = ex; }
						ec.Done.Set();
						break;
					default:
						cmd.Done.Set();
						break;
				}
			}
		} catch {
			ClearDebugState();
			throw;
		}

		ClearDebugState();
		throw new OperationCanceledException("Debug session disposed while paused");
	}

	private void ClearDebugState() {
		_paused = false;
		_currentDebugInfo = null;
		_variableStore.Clear();
		_nextVariableRef = 1;
	}

	private DebugCallFrame[] ReadCallStack(DebugInformation info) {
		var stack = info.CallStack;
		var frames = new DebugCallFrame[stack.Count];
		for (var i = 0; i < stack.Count; i++) {
			var frame = stack[i];
			var loc = frame.Location;
			frames[i] = new DebugCallFrame {
				Id = i,
				Name = frame.FunctionName ?? "(anonymous)",
				Line = loc.Start.Line,
				Column = loc.Start.Column + 1,
			};
		}
		return frames;
	}

	private DebugScopeInfo[] ReadScopes(DebugInformation info, int frameIndex) {
		var stack = info.CallStack;
		if (frameIndex < 0 || frameIndex >= stack.Count)
			throw new ArgumentOutOfRangeException(nameof(frameIndex));
		var frame = stack[frameIndex];
		var chain = frame.ScopeChain;
		var scopes = new DebugScopeInfo[chain.Count];
		for (var i = 0; i < chain.Count; i++) {
			var scope = chain[i];
			var refId = _nextVariableRef++;
			_variableStore[refId] = scope;
			scopes[i] = new DebugScopeInfo {
				Name = scope.ScopeType.ToString(),
				VariablesReference = refId,
				Expensive = scope.ScopeType == DebugScopeType.Global,
			};
		}
		return scopes;
	}

	private DebugVariable[] ReadVariables(int variablesReference) {
		if (!_variableStore.TryGetValue(variablesReference, out var stored))
			throw new InvalidOperationException($"Invalid variable reference: {variablesReference}");

		var wasEvaluating = _evaluating;
		_evaluating = true;
		try {
			return stored switch {
				DebugScope scope => ReadScopeVariables(scope),
				JsArray array => ReadArrayElements(array),
				ObjectInstance obj => ReadObjectProperties(obj),
				_ => throw new InvalidOperationException($"Unexpected variable store type: {stored.GetType()}")
			};
		} finally {
			_evaluating = wasEvaluating;
		}
	}

	private DebugVariable[] ReadScopeVariables(DebugScope scope) {
		var names = scope.BindingNames;
		var variables = new DebugVariable[names.Count];
		for (var i = 0; i < names.Count; i++) {
			var name = names[i];
			var value = scope.GetBindingValue(name);
			variables[i] = MakeVariable(name, value);
		}
		return variables;
	}

	private DebugVariable[] ReadObjectProperties(ObjectInstance obj) {
		var props = new List<DebugVariable>();
		foreach (var kvp in obj.GetOwnProperties()) {
			var name = kvp.Key.AsString();
			var value = kvp.Value.Value;
			if (value.IsUndefined())
				continue;
			props.Add(MakeVariable(name, value));
		}
		return props.ToArray();
	}

	private DebugVariable[] ReadArrayElements(JsArray array) {
		var length = (int)array.Length;
		var variables = new List<DebugVariable>(length + 1);
		for (var i = 0; i < length; i++) {
			var value = array[(uint)i];
			variables.Add(MakeVariable(i.ToString(), value));
		}
		variables.Add(new DebugVariable {
			Name = "length",
			Value = length.ToString(),
			Type = "number",
			VariablesReference = 0,
		});
		return variables.ToArray();
	}

	private DebugVariable MakeVariable(string name, JsValue? value) {
		var refId = 0;
		if (value is ObjectInstance && !value.IsNull() && value is not Function) {
			refId = _nextVariableRef++;
			_variableStore[refId] = value!;
		}
		return new DebugVariable {
			Name = name,
			Value = FormatValue(value),
			Type = GetValueType(value),
			VariablesReference = refId,
		};
	}

	private DebugVariable RunEvaluate(string expression) {
		var wasEvaluating = _evaluating;
		_evaluating = true;
		try {
			var result = _engine.Debugger.Evaluate(expression);
			return MakeVariable("result", result);
		} catch (Jint.Runtime.Debugger.DebugEvaluationException ex) when (ex.InnerException != null) {
			throw ex.InnerException;
		} finally {
			_evaluating = wasEvaluating;
		}
	}

	private string FormatValue(JsValue? value) => value switch {
		null => "undefined",
		JsString s => $"\"{s.AsString()}\"",
		JsNumber n => n.AsNumber().ToString(CultureInfo.InvariantCulture),
		JsBoolean b => b.AsBoolean() ? "true" : "false",
		JsNull => "null",
		JsUndefined => "undefined",
		JsArray a => FormatArrayPreview(a),
		ObjectInstance o => FormatObjectPreview(o),
		_ => value.ToString(),
	};

	private string FormatObjectPreview(ObjectInstance obj) {
		var wasEvaluating = _evaluating;
		_evaluating = true;
		try {
			var sb = new StringBuilder("{");
			var first = true;
			foreach (var kvp in obj.GetOwnProperties()) {
				var propValue = kvp.Value.Value;
				if (propValue.IsUndefined())
					continue;
				if (!first)
					sb.Append(", ");
				first = false;
				sb.Append(kvp.Key.AsString());
				sb.Append(": ");
				sb.Append(FormatPreviewValue(propValue));
				if (sb.Length > 80) {
					sb.Length = 80;
					sb.Append("...");
					break;
				}
			}
			sb.Append('}');
			return sb.ToString();
		} catch {
			return "{...}";
		} finally {
			_evaluating = wasEvaluating;
		}
	}

	private string FormatArrayPreview(JsArray array) {
		var wasEvaluating = _evaluating;
		_evaluating = true;
		try {
			var length = (int)array.Length;
			if (length == 0)
				return "[]";
			var sb = new StringBuilder($"({length}) [");
			for (var i = 0; i < length && i < 5; i++) {
				if (i > 0)
					sb.Append(", ");
				sb.Append(FormatPreviewValue(array[(uint)i]));
				if (sb.Length > 80) {
					sb.Length = 80;
					sb.Append("...");
					break;
				}
			}
			if (length > 5)
				sb.Append(", ...");
			sb.Append(']');
			return sb.ToString();
		} catch {
			return $"Array({array.Length})";
		} finally {
			_evaluating = wasEvaluating;
		}
	}

	private static string FormatPreviewValue(JsValue value) => value switch {
		JsString s => s.AsString().Length > 30 ? $"\"{s.AsString()[..30]}...\"" : $"\"{s.AsString()}\"",
		JsNumber n => n.AsNumber().ToString(CultureInfo.InvariantCulture),
		JsBoolean b => b.AsBoolean() ? "true" : "false",
		JsNull => "null",
		JsUndefined => "undefined",
		JsArray a => $"({a.Length}) [...]",
		_ when value.IsNull() => "null",
		ObjectInstance => "{...}",
		_ => value.ToString(),
	};

	private static string GetValueType(JsValue? value) => value switch {
		null => "undefined",
		JsString => "string",
		JsNumber => "number",
		JsBoolean => "boolean",
		JsNull => "object",
		JsUndefined => "undefined",
		JsArray => "object",
		ObjectInstance => "object",
		_ => value.Type.ToString().ToLowerInvariant(),
	};

	public bool IsPaused => _paused;

	public void Pause() {
		_pauseRequested = true;
	}

	/// <summary>
	/// If a pause was requested, adds temporary breakpoints at all breakable
	/// positions. The first one hit will pause with full execution context
	/// and remove all temporary breakpoints.
	/// </summary>
	public void HandlePauseIfRequested() {
		if (!_pauseRequested)
			return;
		_pauseRequested = false;
		if (_breakablePositions is { Count: > 0 }) {
			foreach (var pos in _breakablePositions)
				_engine.Debugger.BreakPoints.Set(new PauseBreakPoint(pos.Line, pos.Column));
		}
	}

	/// <summary>
	/// Sets a breakpoint, snapping to the nearest breakable position on or after the given position.
	/// Returns the actual (line, column) where the breakpoint was set (1-based),
	/// or null if no breakable position was found.
	/// </summary>
	public (int Line, int Column)? SetBreakpoint(int line, int column = 1, string? condition = null, string? hitCondition = null, string? logMessage = null) {
		var snapped = SnapToBreakablePosition(line, column - 1); // 1-based -> 0-based column
		if (snapped == null)
			return null;

		if (hitCondition != null || logMessage != null) {
			var bp = new GafferBreakPoint(snapped.Value.Line, snapped.Value.Column, condition) {
				LogMessage = logMessage,
			};
			if (hitCondition != null)
				bp.HitConditionFunc = GafferBreakPoint.ParseHitCondition(hitCondition);
			_engine.Debugger.BreakPoints.Set(bp);
		} else {
			_engine.Debugger.BreakPoints.Set(
				new BreakPoint(snapped.Value.Line, snapped.Value.Column, condition));
		}
		return (snapped.Value.Line, snapped.Value.Column + 1); // 0-based -> 1-based for caller
	}

	public void ClearBreakpoints() {
		_engine.Debugger.BreakPoints.Clear();
	}

	private void RemovePauseBreakPoints() {
		var toRemove = new List<BreakLocation>();
		foreach (var bp in _engine.Debugger.BreakPoints) {
			if (bp is PauseBreakPoint)
				toRemove.Add(bp.Location);
		}
		foreach (var loc in toRemove)
			_engine.Debugger.BreakPoints.RemoveAt(loc);
	}

	/// <summary>
	/// Finds the nearest breakable position on or after the requested (line, column).
	/// Column is 0-based (Acornima convention). Positions are sorted by line then column.
	/// </summary>
	private (int Line, int Column)? SnapToBreakablePosition(int line, int column) {
		if (_breakablePositions == null || _breakablePositions.Count == 0)
			return null;

		foreach (var pos in _breakablePositions) {
			if (pos.Line > line || (pos.Line == line && pos.Column >= column))
				return (pos.Line, pos.Column);
		}
		return null;
	}

	public void Continue() {
		if (!_paused)
			throw new InvalidOperationException("Cannot continue when not paused");
		using var cmd = new ContinueCommand();
		_debugCommands.Add(cmd);
		cmd.Done.Wait();
	}

	public void StepInto() {
		if (!_paused)
			throw new InvalidOperationException("Cannot step when not paused");
		using var cmd = new StepCommand { Mode = StepMode.Into };
		_debugCommands.Add(cmd);
		cmd.Done.Wait();
	}

	public void StepOver() {
		if (!_paused)
			throw new InvalidOperationException("Cannot step when not paused");
		using var cmd = new StepCommand { Mode = StepMode.Over };
		_debugCommands.Add(cmd);
		cmd.Done.Wait();
	}

	public void StepOut() {
		if (!_paused)
			throw new InvalidOperationException("Cannot step when not paused");
		using var cmd = new StepCommand { Mode = StepMode.Out };
		_debugCommands.Add(cmd);
		cmd.Done.Wait();
	}

	public DebugCallFrame[] GetCallStack() {
		if (!_paused)
			throw new InvalidOperationException("Cannot inspect when not paused");
		using var cmd = new GetCallStackCommand();
		_debugCommands.Add(cmd);
		cmd.Done.Wait();
		if (cmd.Error != null)
			throw cmd.Error;
		return cmd.Result!;
	}

	public DebugScopeInfo[] GetScopes(int frameIndex) {
		if (!_paused)
			throw new InvalidOperationException("Cannot inspect when not paused");
		using var cmd = new GetScopesCommand { FrameIndex = frameIndex };
		_debugCommands.Add(cmd);
		cmd.Done.Wait();
		if (cmd.Error != null)
			throw cmd.Error;
		return cmd.Result!;
	}

	public DebugVariable[] GetVariables(int variablesReference) {
		if (!_paused)
			throw new InvalidOperationException("Cannot inspect when not paused");
		using var cmd = new GetVariablesCommand { VariablesReference = variablesReference };
		_debugCommands.Add(cmd);
		cmd.Done.Wait();
		if (cmd.Error != null)
			throw cmd.Error;
		return cmd.Result!;
	}

	public DebugVariable Evaluate(string expression) {
		if (!_paused)
			throw new InvalidOperationException("Cannot evaluate when not paused");
		using var cmd = new EvaluateCommand { Expression = expression };
		_debugCommands.Add(cmd);
		cmd.Done.Wait();
		if (cmd.Error != null)
			throw cmd.Error;
		return cmd.Result!;
	}

	private abstract class DebugCommand : IDisposable {
		public ManualResetEventSlim Done { get; } = new(false);
		public Exception? Error { get; set; }
		public void Dispose() => Done.Dispose();
	}

	private sealed class ContinueCommand : DebugCommand;

	private sealed class StepCommand : DebugCommand {
		public required StepMode Mode { get; init; }
	}

	private sealed class EvaluateCommand : DebugCommand {
		public required string Expression { get; init; }
		public DebugVariable? Result { get; set; }
	}

	private sealed class PauseBreakPoint : BreakPoint {
		public PauseBreakPoint(int line, int column) : base(line, column) { }
	}

	private sealed class GafferBreakPoint : BreakPoint {
		public GafferBreakPoint(int line, int column, string? condition = null)
			: base(line, column, condition) { }

		public int HitCount { get; set; }
		public Func<int, bool>? HitConditionFunc { get; set; }
		public string? LogMessage { get; set; }

		public static Func<int, bool>? ParseHitCondition(string hitCondition) {
			var trimmed = hitCondition.Trim();
			if (string.IsNullOrEmpty(trimmed))
				return null;

			// Parse patterns like ">= 5", "% 3", "= 10", "5" (shorthand for "= 5")
			if (int.TryParse(trimmed, out var exact))
				return count => count == exact;

			foreach (var (prefix, factory) in HitConditionParsers) {
				if (trimmed.StartsWith(prefix) && int.TryParse(trimmed[prefix.Length..].Trim(), out var value))
					return factory(value);
			}
			return null;
		}

		private static readonly (string Prefix, Func<int, Func<int, bool>> Factory)[] HitConditionParsers = [
			(">=", v => c => c >= v),
			("<=", v => c => c <= v),
			(">", v => c => c > v),
			("<", v => c => c < v),
			("==", v => c => c == v),
			("=", v => c => c == v),
			("%", v => v > 0 ? c => c % v == 0 : _ => false),
		];
	}

	private string EvaluateLogMessage(string template) {
		var sb = new StringBuilder();
		var i = 0;
		while (i < template.Length) {
			if (template[i] == '{') {
				var end = template.IndexOf('}', i + 1);
				if (end > i) {
					var expr = template[(i + 1)..end];
					try {
						var wasEvaluating = _evaluating;
						_evaluating = true;
						try {
							var result = _engine.Debugger.Evaluate(expr);
							sb.Append(result.IsString() ? result.AsString() : FormatValue(result));
						} finally {
							_evaluating = wasEvaluating;
						}
					} catch {
						sb.Append($"{{{expr}}}");
					}
					i = end + 1;
					continue;
				}
			}
			sb.Append(template[i]);
			i++;
		}
		return sb.ToString();
	}

	private readonly record struct BreakablePosition(int Line, int Column);

	private sealed class BreakablePositionCollector : Acornima.AstVisitor {
		public readonly List<BreakablePosition> Positions = new();

		public override object? Visit(Acornima.Ast.Node node) {
			if (node is Acornima.Ast.Statement and not Acornima.Ast.BlockStatement)
				Positions.Add(new BreakablePosition(node.Location.Start.Line, node.Location.Start.Column));
			return base.Visit(node);
		}

		protected override object? VisitForStatement(Acornima.Ast.ForStatement node) {
			if (node.Test != null)
				Positions.Add(new BreakablePosition(node.Test.Location.Start.Line, node.Test.Location.Start.Column));
			if (node.Update != null)
				Positions.Add(new BreakablePosition(node.Update.Location.Start.Line, node.Update.Location.Start.Column));
			return base.VisitForStatement(node);
		}

		protected override object? VisitForInStatement(Acornima.Ast.ForInStatement node) {
			Positions.Add(new BreakablePosition(node.Left.Location.Start.Line, node.Left.Location.Start.Column));
			return base.VisitForInStatement(node);
		}

		protected override object? VisitForOfStatement(Acornima.Ast.ForOfStatement node) {
			Positions.Add(new BreakablePosition(node.Left.Location.Start.Line, node.Left.Location.Start.Column));
			return base.VisitForOfStatement(node);
		}

		protected override object? VisitFunctionBody(Acornima.Ast.FunctionBody node) {
			Positions.Add(new BreakablePosition(node.Location.End.Line, node.Location.End.Column));
			return base.VisitFunctionBody(node);
		}
	}

	private sealed class GetCallStackCommand : DebugCommand {
		public DebugCallFrame[]? Result { get; set; }
	}

	private sealed class GetScopesCommand : DebugCommand {
		public required int FrameIndex { get; init; }
		public DebugScopeInfo[]? Result { get; set; }
	}

	private sealed class GetVariablesCommand : DebugCommand {
		public required int VariablesReference { get; init; }
		public DebugVariable[]? Result { get; set; }
	}

	// -- Nested types below --

	private sealed class TimeConstraint : Constraint {
		private readonly TimeSpan _compilationTimeout;
		private readonly TimeSpan _executionTimeout;
		private TimeSpan _start;
		private TimeSpan _timeout;
		private bool _executing;
		public bool Disabled { get; set; }

		public TimeConstraint(TimeSpan compilationTimeout, TimeSpan executionTimeout) {
			_compilationTimeout = compilationTimeout;
			_executionTimeout = executionTimeout;
			_timeout = _compilationTimeout;
		}

		public void Compiling() { _timeout = _compilationTimeout; _executing = false; }
		public void Executing() { _timeout = _executionTimeout; _executing = true; }

		public override void Reset() => _start = Sw.Elapsed;

		public override void Check() {
			if (Disabled)
				return;
			var elapsed = Sw.Elapsed - _start;
			if (elapsed >= _timeout) {
				if (Debugger.IsAttached)
					return;
				throw new Errors.TimeConstraintException(
					!_executing,
					(int)elapsed.TotalMilliseconds,
					(int)_timeout.TotalMilliseconds);
			}
		}
	}

	internal sealed class ProjectionRuntime : ObjectInstance {
		private readonly Dictionary<string, ScriptFunction> _handlers;
		private readonly List<(TransformType, ScriptFunction)> _transforms;
		private readonly List<ScriptFunction> _createdHandlers;
		private ScriptFunction? _init;
		private ScriptFunction? _initShared;
		private ScriptFunction? _any;
		private ScriptFunction? _deleted;
		private ScriptFunction? _partitionFunction;

		private readonly JsValue _whenInstance;
		private readonly JsValue _partitionByInstance;
		private readonly JsValue _outputStateInstance;
		private readonly JsValue _foreachStreamInstance;
		private readonly JsValue _transformByInstance;
		private readonly JsValue _filterByInstance;
		private readonly JsValue _outputToInstance;
		private readonly JsValue _definesStateTransformInstance;

		private readonly SourceDefinitionBuilder _definitionBuilder;
		private readonly JsonParser _parser;

		private static readonly Dictionary<string, Action<ProjectionRuntime>> PossibleProperties = new() {
			["when"] = i => i.FastAddProperty("when", i._whenInstance, true, false, true),
			["partitionBy"] = i => i.FastAddProperty("partitionBy", i._partitionByInstance, true, false, true),
			["outputState"] = i => i.FastAddProperty("outputState", i._outputStateInstance, true, false, true),
			["foreachStream"] = i => i.FastAddProperty("foreachStream", i._foreachStreamInstance, true, false, true),
			["transformBy"] = i => i.FastAddProperty("transformBy", i._transformByInstance, true, false, true),
			["filterBy"] = i => i.FastAddProperty("filterBy", i._filterByInstance, true, false, true),
			["outputTo"] = i => i.FastAddProperty("outputTo", i._outputToInstance, true, false, true),
			["$defines_state_transform"] = i => i.FastAddProperty("$defines_state_transform", i._definesStateTransformInstance, true, false, true),
		};

		private static readonly Dictionary<string, string[]> AvailableProperties = new() {
			// fromStream / fromStreams keep `foreachStream` reachable only so
			// ForEachStream can throw a friendly error; the source doesn't
			// actually support it (see ForEachStream).
			["fromStream"] = ["when", "partitionBy", "outputState", "foreachStream"],
			["fromAll"] = ["when", "partitionBy", "outputState", "foreachStream"],
			["fromStreams"] = ["when", "partitionBy", "outputState", "foreachStream"],
			["fromCategory"] = ["when", "partitionBy", "outputState", "foreachStream"],
			["when"] = ["transformBy", "filterBy", "outputState", "outputTo", "$defines_state_transform"],
			["foreachStream"] = ["when"],
			["outputState"] = ["transformBy", "filterBy", "outputTo"],
			["partitionBy"] = ["when"],
			["transformBy"] = ["transformBy", "filterBy", "outputState", "outputTo"],
			["filterBy"] = ["transformBy", "filterBy", "outputState", "outputTo"],
			["outputTo"] = Array.Empty<string>(),
			["execution"] = Array.Empty<string>(),
		};

		private static readonly Dictionary<string, Action<SourceDefinitionBuilder, JsValue>> Setters =
			new(StringComparer.OrdinalIgnoreCase)
			{
				{ "$includeLinks", (o, v) => o.SetIncludeLinks(v.IsBoolean() ? v.AsBoolean() : throw new ArgumentException("Invalid value for option '$includeLinks': expected a boolean")) },
				{ "reorderEvents", (o, v) => o.SetReorderEvents(v.IsBoolean() ? v.AsBoolean() : throw new ArgumentException("Invalid value for option 'reorderEvents': expected a boolean")) },
				{ "processingLag", (o, v) => o.SetProcessingLag(v.IsNumber() ? (int)v.AsNumber() : throw new ArgumentException("Invalid value for option 'processingLag': expected a number")) },
				{ "resultStreamName", (o, v) => o.SetResultStreamNameOption(v.IsString() ? v.AsString() : throw new ArgumentException("Invalid value for option 'resultStreamName': expected a string")) },
				{ "biState", (o, v) => o.SetIsBiState(v.IsBoolean() ? v.AsBoolean() : throw new ArgumentException("Invalid value for option 'biState': expected a boolean")) },
				{ "trackEmittedStreams", (o, v) => o.SetTrackEmittedStreams(v.IsBoolean() ? v.AsBoolean() : throw new ArgumentException("Invalid value for option 'trackEmittedStreams': expected a boolean")) },
			};

		private readonly List<string> _definitionFunctions;

		// The source method last applied (fromStream / fromStreams / fromAll /
		// fromCategory), so ForEachStream can reject foreachStream() on sources
		// that don't support it.
		private string? _sourceMethod;

		public ProjectionRuntime(Engine engine, SourceDefinitionBuilder builder) : base(engine) {
			_definitionBuilder = builder;
			_handlers = new Dictionary<string, ScriptFunction>(StringComparer.Ordinal);
			_createdHandlers = new List<ScriptFunction>();
			_transforms = new List<(TransformType, ScriptFunction)>();
			_parser = new JsonParser(engine);
			_definitionFunctions = new List<string>();

			AddDefinitionFunction("options", SetOptions, 1);
			AddDefinitionFunction("fromStream", FromStream, 1);
			AddDefinitionFunction("fromCategory", FromCategory, 4);
			AddDefinitionFunction("fromCategories", FromCategory, 4);
			AddDefinitionFunction("fromAll", FromAll, 0);
			AddDefinitionFunction("fromStreams", FromStreams, 1);
			AddDefinitionFunction("on_event", OnEvent, 1);
			AddDefinitionFunction("on_any", OnAny, 1);

			_whenInstance = new ClrFunction(engine, "when", When, 1);
			_partitionByInstance = new ClrFunction(engine, "partitionBy", PartitionBy, 1);
			_outputStateInstance = new ClrFunction(engine, "outputState", OutputState, 1);
			_foreachStreamInstance = new ClrFunction(engine, "foreachStream", ForEachStream, 1);
			_transformByInstance = new ClrFunction(engine, "transformBy", TransformBy, 1);
			_filterByInstance = new ClrFunction(engine, "filterBy", FilterBy, 1);
			_outputToInstance = new ClrFunction(engine, "outputTo", OutputTo, 1);
			_definesStateTransformInstance = new ClrFunction(engine, "$defines_state_transform", DefinesStateTransform);
		}

		private void AddDefinitionFunction(string name, Func<JsValue, JsValue[], JsValue> func, int length) {
			_definitionFunctions.Add(name);
			_engine.Global.FastAddProperty(name, new ClrFunction(_engine, name, func, length), true, false, true);
		}

		private JsValue FromStream(JsValue _, JsValue[] parameters) {
			var stream = parameters.At(0);
			if (stream is not JsString)
				throw new ArgumentException("fromStream expects a string argument");
			_definitionBuilder.FromStream(stream.AsString());
			RestrictProperties("fromStream");
			return this;
		}

		private JsValue FromCategory(JsValue thisValue, JsValue[] parameters) {
			if (parameters.Length == 0)
				return this;
			if (parameters.Length == 1 && parameters.At(0).IsArray()) {
				foreach (var cat in parameters.At(0).AsArray()) {
					if (cat is not JsString s)
						throw new ArgumentException("fromCategory expects string arguments");
					_definitionBuilder.FromStream($"$ce-{s.AsString()}");
				}
			} else if (parameters.Length > 1) {
				foreach (var cat in parameters) {
					if (cat is not JsString s)
						throw new ArgumentException("fromCategory expects string arguments");
					_definitionBuilder.FromStream($"$ce-{s.AsString()}");
				}
			} else {
				var p0 = parameters.At(0);
				if (p0 is not JsString s)
					throw new ArgumentException("fromCategory expects a string argument");
				_definitionBuilder.FromCategory(s.AsString());
			}
			RestrictProperties("fromCategory");
			return this;
		}

		private JsValue When(JsValue thisValue, JsValue[] parameters) {
			if (parameters.At(0) is ObjectInstance handlers) {
				foreach (var kvp in handlers.GetOwnProperties()) {
					if (kvp.Key.IsString() && kvp.Value.Value is ScriptFunction sf)
						AddHandler(kvp.Key.AsString(), sf);
				}
			}
			_definitionBuilder.SetDefinesFold();
			RestrictProperties("when");
			return this;
		}

		private JsValue PartitionBy(JsValue thisValue, JsValue[] parameters) {
			if (parameters.At(0) is ScriptFunction partitionFunction) {
				_definitionBuilder.SetByCustomPartitions();
				_partitionFunction = partitionFunction;
				RestrictProperties("partitionBy");
				return this;
			}
			throw new ArgumentException("partitionBy expects a function");
		}

		private JsValue ForEachStream(JsValue thisValue, JsValue[] parameters) {
			if (_sourceMethod is "fromStream" or "fromStreams")
				throw new ArgumentException(
					"foreachStream() is only supported with fromAll() and fromCategory()");
			_definitionBuilder.SetByStream();
			RestrictProperties("foreachStream");
			return this;
		}

		private JsValue OutputState(JsValue thisValue, JsValue[] parameters) {
			RestrictProperties("outputState");
			_definitionBuilder.SetOutputState();
			return this;
		}

		private JsValue OutputTo(JsValue thisValue, JsValue[] parameters) {
			if (parameters.Length is not (1 or 2))
				throw new ArgumentException("outputTo expects 1 or 2 string arguments");
			if (!parameters.At(0).IsString())
				throw new ArgumentException("outputTo expects a string for resultStream");
			if (parameters.Length == 2 && !parameters.At(1).IsString())
				throw new ArgumentException("outputTo expects a string for partitionResultStreamPattern");
			_definitionBuilder.SetResultStreamNameOption(parameters.At(0).AsString());
			if (parameters.Length == 2)
				_definitionBuilder.SetPartitionResultStreamNamePatternOption(parameters.At(1).AsString());
			RestrictProperties("outputTo");
			return this;
		}

		private JsValue DefinesStateTransform(JsValue thisValue, JsValue[] parameters) {
			_definitionBuilder.SetDefinesStateTransform();
			_definitionBuilder.SetOutputState();
			return Undefined;
		}

		private JsValue FilterBy(JsValue thisValue, JsValue[] parameters) {
			if (parameters.At(0) is ScriptFunction fi) {
				_definitionBuilder.SetDefinesStateTransform();
				_definitionBuilder.SetOutputState();
				_transforms.Add((TransformType.Filter, fi));
				RestrictProperties("filterBy");
				return this;
			}
			throw new ArgumentException("filterBy expects a function");
		}

		private JsValue TransformBy(JsValue thisValue, JsValue[] parameters) {
			if (parameters.At(0) is ScriptFunction fi) {
				_definitionBuilder.SetDefinesStateTransform();
				_definitionBuilder.SetOutputState();
				_transforms.Add((TransformType.Transform, fi));
				RestrictProperties("transformBy");
				return this;
			}
			throw new ArgumentException("transformBy expects a function");
		}

		private JsValue OnEvent(JsValue thisValue, JsValue[] parameters) {
			if (parameters.Length != 2)
				throw new ArgumentException("on_event expects 2 arguments: eventName and handler function");
			if (!parameters.At(0).IsString())
				throw new ArgumentException("on_event expects a string for eventName");
			if (parameters.At(1) is not ScriptFunction fi)
				throw new ArgumentException("on_event expects a function for handler");
			AddHandler(parameters.At(0).AsString(), fi);
			return Undefined;
		}

		private JsValue OnAny(JsValue thisValue, JsValue[] parameters) {
			if (parameters.Length != 1)
				throw new ArgumentException("on_any expects 1 argument: handler function");
			if (parameters.At(0) is not ScriptFunction fi)
				throw new ArgumentException("on_any expects a function for handler");
			AddHandler("$any", fi);
			return Undefined;
		}

		private void AddHandler(string name, ScriptFunction handler) {
			switch (name) {
				case "$init":
					_init = handler;
					break;
				case "$initShared":
					_definitionBuilder.SetIsBiState(true);
					_initShared = handler;
					break;
				case "$any":
					_any = handler;
					_definitionBuilder.AllEvents();
					break;
				case "$created":
					_createdHandlers.Add(handler);
					break;
				case "$deleted" when !_definitionBuilder.IsBiState:
					_definitionBuilder.SetHandlesStreamDeletedNotifications();
					_deleted = handler;
					break;
				case "$deleted" when _definitionBuilder.IsBiState:
					throw new Exception("Cannot handle deletes in bi-state projections");
				default:
					_definitionBuilder.NotAllEvents();
					_definitionBuilder.IncludeEvent(name);
					_handlers.Add(name, handler);
					break;
			}
		}

		private void RestrictProperties(string state) {
			if (state is "fromStream" or "fromStreams" or "fromAll" or "fromCategory")
				_sourceMethod = state;
			var allowed = AvailableProperties[state];
			var current = GetOwnPropertyKeys();
			foreach (var p in current) {
				if (!allowed.Contains(p.AsString()))
					RemoveOwnProperty(p);
			}
			foreach (var p in allowed) {
				if (!HasOwnProperty(p))
					PossibleProperties[p](this);
			}
		}

		public JsValue InitializeState() =>
			_init == null ? new JsObject(Engine) : _init.Call(JsValue.Undefined, []);

		public JsValue InitializeSharedState() =>
			_initShared == null ? new JsObject(Engine) : _initShared.Call(JsValue.Undefined, []);

		public JsValue Handle(JsValue state, EventEnvelope eventEnvelope) {
			JsValue newState;
			if (_handlers.TryGetValue(eventEnvelope.EventType, out var handler))
				newState = handler.Call(JsValue.Undefined, [state, FromObject(Engine, eventEnvelope)]);
			else if (_any != null)
				newState = _any.Call(JsValue.Undefined, [state, FromObject(Engine, eventEnvelope)]);
			else
				newState = eventEnvelope.BodyRaw;
			return newState == Undefined ? state : newState;
		}

		public JsValue TransformStateToResult(JsValue state) {
			foreach (var (type, transform) in _transforms) {
				switch (type) {
					case TransformType.Transform:
						state = transform.Call(JsValue.Undefined, [state]);
						break;
					case TransformType.Filter:
						var result = transform.Call(JsValue.Undefined, [state]);
						if (!(result.IsBoolean() && result.AsBoolean()) || result == Null || result == Undefined)
							return Null;
						break;
					default:
						throw new InvalidOperationException($"Unknown transform type: {type}");
				}
				if (state == Null || state == Undefined)
					return Null;
			}
			return state;
		}

		private JsValue FromAll(JsValue _, JsValue[] __) {
			_definitionBuilder.FromAll();
			RestrictProperties("fromAll");
			return this;
		}

		private JsValue FromStreams(JsValue _, JsValue[] parameters) {
			if (parameters.Length == 1 && parameters.At(0).IsArray()) {
				foreach (var stream in parameters.At(0).AsArray()) {
					if (stream is not JsString s)
						throw new ArgumentException("fromStreams expects string arguments");
					_definitionBuilder.FromStream(s.AsString());
				}
			} else {
				for (var i = 0; i < parameters.Length; i++) {
					if (parameters[i] is not JsString s)
						throw new ArgumentException("fromStreams expects string arguments");
					_definitionBuilder.FromStream(s.AsString());
				}
			}
			RestrictProperties("fromStreams");
			return this;
		}

		private JsValue SetOptions(JsValue thisValue, JsValue[] parameters) {
			if (parameters.At(0) is ObjectInstance opts) {
				foreach (var kvp in opts.GetOwnProperties()) {
					if (Setters.TryGetValue(kvp.Key.AsString(), out var setter))
						setter(_definitionBuilder, kvp.Value.Value);
					else
						throw new ArgumentException($"Unrecognized option: {kvp.Key}");
				}
			}
			return Undefined;
		}

		public JsValue GetPartition(EventEnvelope envelope) =>
			_partitionFunction != null
				? _partitionFunction.Call(JsValue.Undefined, [envelope])
				: Null;

		public void HandleCreated(JsValue state, EventEnvelope envelope) {
			for (var i = 0; i < _createdHandlers.Count; i++)
				_createdHandlers[i].Call(JsValue.Undefined, [state, envelope]);
		}

		public void HandleDeleted(JsValue state, string partition, bool isSoftDelete) {
			_deleted?.Call(JsValue.Undefined, [state, Null, new JsString(partition), isSoftDelete ? JsBoolean.True : JsBoolean.False]);
		}

		public JsonParser SwitchToExecutionMode() {
			RestrictProperties("execution");
			foreach (var globalProp in _definitionFunctions)
				_engine.Global.RemoveOwnProperty(globalProp);
			return _parser;
		}

		private enum TransformType { None, Filter, Transform }
	}

	internal sealed class EventEnvelope : ObjectInstance {
		private readonly JsonParser _parser;
		private readonly JintProjectionHandler _parent;

		public string StreamId {
			set => SetOwnProperty("streamId", new PropertyDescriptor(value, false, true, false));
		}

		public long SequenceNumber {
			set => SetOwnProperty("sequenceNumber", new PropertyDescriptor(value, false, true, false));
		}

		public string EventType {
			get => _parent.AsString(Get("eventType"), false) ?? "";
			set => SetOwnProperty("eventType", new PropertyDescriptor(value, false, true, false));
		}

		public JsValue Body {
			get {
				if (TryGetValue("body", out var value) && value is ObjectInstance)
					return value;
				return EnsureBody(out var obj) ? obj : Undefined;
			}
		}

		private bool EnsureBody(out JsValue objectInstance) {
			if (IsJson && TryGetValue("bodyRaw", out var raw) && raw is not JsUndefined) {
				JsValue body;
				try {
					body = raw.IsNull() ? raw : _parser.Parse(raw.AsString());
				} catch (Exception ex) {
					throw new Errors.MalformedEventDataException("data", ex);
				}
				var pd = new PropertyDescriptor(body, false, true, false);
				SetOwnProperty("body", pd);
				SetOwnProperty("data", pd);
				objectInstance = DiagnosticCatalog.EventBodyCast.FiresAt(_parent._quirksVersion)
					? RunQuirkyPath<JsValue>(DiagnosticCatalog.EventBodyCast, () => EventBodyCastBehaviour.Apply(body))
					: body;
				return true;
			}
			objectInstance = Undefined;
			return false;
		}

		public bool IsJson {
			get => Get("isJson").AsBoolean();
			set => SetOwnProperty("isJson", new PropertyDescriptor(value, false, true, false));
		}

		public string? BodyRaw {
			get => _parent.AsString(Get("bodyRaw"), false);
			set => SetOwnProperty("bodyRaw", new PropertyDescriptor(value, false, true, false));
		}

		private JsValue Metadata {
			get {
				if (TryGetValue("metadata", out var value) && value is ObjectInstance)
					return value;
				return EnsureMetadata(out value) ? value : Undefined;
			}
		}

		private bool EnsureMetadata(out JsValue value) {
			if (TryGetValue("metadataRaw", out var raw) && raw is not JsUndefined) {
				JsValue metadata;
				try {
					metadata = raw.IsNull() ? raw : _parser.Parse(raw.AsString());
				} catch (Exception ex) {
					throw new Errors.MalformedEventDataException("metadata", ex);
				}
				SetOwnProperty("metadata", new PropertyDescriptor(metadata, false, true, false));
				value = metadata;
				return true;
			}
			value = Undefined;
			return false;
		}

		public string? MetadataRaw {
			set => FastSetProperty("metadataRaw", new PropertyDescriptor(value, false, true, false));
		}

		private JsValue LinkMetadata {
			get {
				if (TryGetValue("linkMetadata", out var value) && value is ObjectInstance)
					return value;
				return EnsureLinkMetadata(out value) ? value : Undefined;
			}
		}

		private bool EnsureLinkMetadata(out JsValue value) {
			if (TryGetValue("linkMetadataRaw", out var raw) && raw is not JsUndefined) {
				JsValue metadata;
				try {
					metadata = raw.IsNull() ? raw : _parser.Parse(raw.AsString());
				} catch (Exception ex) {
					throw new Errors.MalformedEventDataException("linkMetadata", ex);
				}
				SetOwnProperty("linkMetadata", new PropertyDescriptor(metadata, false, true, false));
				value = metadata;
				return true;
			}
			value = Undefined;
			return false;
		}

		public string? LinkMetadataRaw {
			set => SetOwnProperty("linkMetadataRaw", new PropertyDescriptor(value, false, true, false));
		}

		public string Partition {
			set => SetOwnProperty("partition", new PropertyDescriptor(value, false, true, false));
		}

		public string Category {
			set => SetOwnProperty("category", new PropertyDescriptor(value, false, true, false));
		}

		public DateTime Created {
			set => SetOwnProperty("created", new PropertyDescriptor(value.ToString("o"), false, true, false));
		}

		public string EventId {
			set => SetOwnProperty("eventId", new PropertyDescriptor(value, false, true, false));
		}

		public EventEnvelope(Engine engine, JsonParser parser, JintProjectionHandler parent) : base(engine) {
			_parser = parser;
			_parent = parent;
		}

		public override JsValue Get(JsValue property, JsValue receiver) {
			if (property == "body" || property == "data")
				return Body;
			if (property == "metadata")
				return Metadata;
			if (property == "linkMetadata")
				return LinkMetadata;
			return base.Get(property, receiver);
		}

		public override List<JsValue> GetOwnPropertyKeys(Types types = Types.String | Types.Symbol) =>
			base.GetOwnPropertyKeys(types);

		public override IEnumerable<KeyValuePair<JsValue, PropertyDescriptor>> GetOwnProperties() {
			if (!HasOwnProperty("body"))
				EnsureBody(out _);
			if (!HasOwnProperty("metadata"))
				EnsureMetadata(out _);
			if (!HasOwnProperty("linkMetadata"))
				EnsureLinkMetadata(out _);
			return base.GetOwnProperties();
		}
	}

	internal sealed class Serializer {
		private readonly WriteState[] _iterators;
		private readonly ArrayBufferWriter<byte> _bufferWriter;
		private readonly Utf8JsonWriter _writer;
		private readonly Dictionary<string, JsonEncodedText> _knownPropertyNames;
		private readonly bool _nonFiniteWritesNull;
		private int _depth;

		public Serializer(KurrentDbVersion? quirksVersion) {
			// The clean (post-fix) path writes JSON null for NaN/Infinity,
			// matching JSON.stringify. The quirky path throws.
			_nonFiniteWritesNull = !DiagnosticCatalog.SerializeNonFinite.FiresAt(quirksVersion);
			_iterators = new WriteState[64];
			_bufferWriter = new ArrayBufferWriter<byte>(1024 * 1024);
			_writer = new Utf8JsonWriter(_bufferWriter, new JsonWriterOptions {
				Indented = false,
				SkipValidation = true,
				Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping,
			});
			_knownPropertyNames = new Dictionary<string, JsonEncodedText>();
		}

		public ReadOnlyMemory<byte> Serialize(JsValue value) {
			_depth = 0;
			_bufferWriter.Clear();
			_writer.Reset();

			if (value is JsArray array)
				_iterators[_depth] = new WriteState(array);
			else if (value is ObjectInstance oi)
				_iterators[_depth] = new WriteState(oi);
			else
				_iterators[_depth] = new WriteState(value);

			ref var current = ref _iterators[0];
			while (current.Write(_writer, ref _depth, _iterators, _knownPropertyNames, _nonFiniteWritesNull))
				current = ref _iterators[_depth];

			_writer.Flush();
			return _bufferWriter.WrittenMemory;
		}

		private struct WriteState {
			private enum Type { Complete, Array, Object, Primitive }

			private static readonly IEnumerator<KeyValuePair<JsValue, PropertyDescriptor>> EmptyIterator = new NoopIterator();

			private sealed class NoopIterator : IEnumerator<KeyValuePair<JsValue, PropertyDescriptor>> {
				public KeyValuePair<JsValue, PropertyDescriptor> Current => default;
				object? IEnumerator.Current => default;
				public void Dispose() { }
				public bool MoveNext() => false;
				public void Reset() { }
			}

			public WriteState(JsArray instance) {
				_position = -1;
				_length = (int)instance.Length;
				_instance = instance;
				_type = Type.Array;
				_started = false;
				_iterator = EmptyIterator;
			}

			public WriteState(ObjectInstance instance) {
				_position = -1;
				_length = -1;
				_instance = JsValue.Null;
				_type = Type.Object;
				_started = false;
				_iterator = instance.GetOwnProperties().GetEnumerator();
			}

			public WriteState(JsValue instance) {
				if (instance.Type == Types.Object)
					throw new ArgumentException("Primitive overload called for object instance");
				_position = -1;
				_length = -1;
				_instance = instance;
				_type = Type.Primitive;
				_started = false;
				_iterator = EmptyIterator;
			}

			private readonly JsValue _instance;
			private readonly IEnumerator<KeyValuePair<JsValue, PropertyDescriptor>> _iterator;
			private readonly Type _type;
			private readonly int _length;
			private int _position;
			private bool _started;

			[MethodImpl(MethodImplOptions.AggressiveInlining | MethodImplOptions.AggressiveOptimization)]
			public bool Write(
				Utf8JsonWriter writer, ref int depth, WriteState[] writeStates,
				Dictionary<string, JsonEncodedText> knownPropertyNames, bool nonFiniteWritesNull) {
				switch (_type) {
					case Type.Array:
						if (_position == -1) { writer.WriteStartArray(); _position++; }
						var instance = (JsArray)_instance;
						for (; _position < _length; _position++) {
							var value = instance[(uint)_position];
							if (value.Type == Types.Object) {
								writeStates[++depth] = value is JsArray ai ? new WriteState(ai) : new WriteState(value.AsObject());
								_position++;
								return true;
							}
							SerializePrimitive(value, writer, nonFiniteWritesNull);
						}
						writer.WriteEndArray();
						break;
					case Type.Object:
						if (!_started) { writer.WriteStartObject(); _started = true; }
						while (_iterator.MoveNext()) {
							var (name, propertyDescriptor) = _iterator.Current;
							var value = propertyDescriptor.Value;
							if (value.Type == Types.Undefined)
								continue;
							WriteMaybeCachedPropertyName(name.AsString(), knownPropertyNames, writer);
							if (value.Type == Types.Object) {
								writeStates[++depth] = value is JsArray ai ? new WriteState(ai) : new WriteState(value.AsObject());
								_position++;
								return true;
							}
							SerializePrimitive(value, writer, nonFiniteWritesNull);
						}
						writer.WriteEndObject();
						break;
					case Type.Primitive:
						SerializePrimitive(_instance, writer, nonFiniteWritesNull);
						break;
				}
				writeStates[depth] = default;
				depth--;
				return depth >= 0;
			}
		}

		[MethodImpl(MethodImplOptions.AggressiveInlining | MethodImplOptions.AggressiveOptimization)]
		private static void WriteMaybeCachedPropertyName(
			string name, Dictionary<string, JsonEncodedText> knownPropertyNames, Utf8JsonWriter writer) {
			if (!knownPropertyNames.TryGetValue(name, out var propertyName)) {
				propertyName = JsonEncodedText.Encode(name);
				if (knownPropertyNames.Count < 1000)
					knownPropertyNames.Add(name, propertyName);
			}
			writer.WritePropertyName(propertyName);
		}

		[MethodImpl(MethodImplOptions.AggressiveInlining | MethodImplOptions.AggressiveOptimization)]
		private static void SerializePrimitive(JsValue value, Utf8JsonWriter writer, bool nonFiniteWritesNull) {
			switch (value.Type) {
				case Types.Null:
				case Types.Undefined:
				case Types.Empty:
					writer.WriteNullValue();
					break;
				case Types.Boolean:
					writer.WriteBooleanValue(ReferenceEquals(value, JsBoolean.False) ? false : true);
					break;
				case Types.Number:
					var num = value.AsNumber();
					if (double.IsNaN(num) || double.IsInfinity(num)) {
						if (nonFiniteWritesNull)
							writer.WriteNullValue();
						else
							SerializeNonFiniteBehaviour.Throw(num);
					} else {
						writer.WriteNumberValue(num);
					}
					break;
				case Types.BigInt:
					writer.WriteStringValue(value.ToString());
					break;
				case Types.String:
					writer.WriteStringValue(value.AsString());
					break;
				default:
					throw new Errors.StateSerializationException($"{value.Type} is not serializable as JSON", "", "", 0);
			}
		}
	}
}

internal static class ObjectInstanceExtensions {
	public static void FastAddProperty(this ObjectInstance target, string name, JsValue value, bool writable, bool enumerable, bool configurable) {
		target.FastSetProperty(name, new PropertyDescriptor(value, writable, enumerable, configurable));
	}
}
