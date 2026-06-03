using Gaffer.Sdk.Versioning;

namespace Gaffer.Sdk.Diagnostics;

/// <summary>
/// Every diagnostic gaffer can emit, keyed by code - the single source of truth for codes,
/// severities, messages, and docs across the compile-time rules, the runtime quirk
/// behaviours, and the generated docs. Adding a diagnostic means adding a descriptor here.
/// <para>
/// <c>quirk.*</c> descriptors reproduce KurrentDB engine bugs; references point at upstream
/// <c>KurrentDB.Projections.JavaScript/Services/Interpreted/JintProjectionStateHandler.cs</c>
/// (and <c>KurrentDB.Projections.V1/.../ReaderStrategy.cs</c>) at tag <c>v26.1.0</c>.
/// </para>
/// </summary>
public static class DiagnosticCatalog {
	// ---------- quirk.* : reproduced KurrentDB engine bugs, gated by quirksVersion ----------

	/// <summary>
	/// <c>linkStreamTo(stream, link, metadata)</c> with a third argument crashes upstream: the
	/// metadata branch reads <c>parameters.At(4)</c> while the arity check gates on length 3, so
	/// the read is out of bounds and throws. The two-arg form works; metadata is never captured.
	/// </summary>
	public static readonly DiagnosticDescriptor LinkStreamToOutOfBoundsParameters = new() {
		Code = "quirk.linkStreamTo.outOfBoundsParameters",
		Class = DiagnosticClass.Quirk,
		Severity = DiagnosticSeverity.Error,
		Message = "linkStreamTo with metadata (3+ args) crashes due to an upstream parameter-indexing bug; metadata is never captured.",
		Docs = "`linkStreamTo(stream, link, metadata)` with a third (metadata) argument crashes in the KurrentDB projection engine - the metadata branch reads an out-of-bounds parameter and throws. The two-argument form works; metadata is never captured. gaffer reproduces the crash.",
		FixedIn = null, // no upstream fix as of 26.2.0
	};

	/// <summary>
	/// Multi-arg <c>log()</c> stacks upstream bugs: the separator gate is <c>i &gt; 1</c> rather
	/// than <c>&gt; 0</c>, the separator is <c>" ,"</c>, and primitives are logged inside the
	/// loop. Net: <c>log("a", "b", "c")</c> emits three lines; <c>log({a:1}, {b:2})</c> emits one
	/// line <c>"{...} ,{...}"</c>.
	/// </summary>
	public static readonly DiagnosticDescriptor LogMultiParam = new() {
		Code = "quirk.log.multiParam",
		Class = DiagnosticClass.Quirk,
		Severity = DiagnosticSeverity.Warning,
		Message = "log() with multiple args produces unexpected output due to an upstream bug: primitives become separate log lines and objects use a ' ,' separator.",
		Docs = "`log()` with more than one argument behaves oddly in the KurrentDB engine: primitive arguments are emitted as separate log lines, and objects are joined with a ` ,` separator. Pass a single pre-formatted argument to avoid surprises.",
		FixedIn = null, // no upstream fix as of 26.2.0
	};

	/// <summary>
	/// Accessing <c>event.body</c> throws when the parsed body is a non-object JSON value (null,
	/// or a primitive like a number, string, or boolean): the upstream <c>EnsureBody</c> casts to
	/// <c>ObjectInstance</c> without a type check. Fixed upstream in PR #5610 by removing the cast.
	/// </summary>
	public static readonly DiagnosticDescriptor EventBodyCast = new() {
		Code = "quirk.event.bodyCast",
		Class = DiagnosticClass.Quirk,
		Severity = DiagnosticSeverity.Error,
		Message = "Accessing event.body throws for a non-object event body (null, or a primitive like a number or string); use event.bodyRaw instead.",
		Docs = "Accessing `event.body` throws in the KurrentDB engine when the event body is a non-object JSON value - null, or a primitive like a number or string. The upstream `EnsureBody` casts to an object without a type check. Use `event.bodyRaw` and parse it yourself. (gaffer's JS testing library normalizes a `data: null` event to an absent body, so a null body won't reproduce the throw there.)",
		FixedIn = new KurrentDbVersion(26, 2, 0), // PR #5610, shipped 26.2.0
	};

	/// <summary>
	/// The JSON serializer throws on <c>NaN</c> or <c>+/-Infinity</c> in state because
	/// <c>Utf8JsonWriter.WriteNumberValue</c> rejects non-finite doubles regardless of
	/// <c>SkipValidation</c>. Fixed upstream in PR #5610 by writing JSON <c>null</c> instead.
	/// </summary>
	public static readonly DiagnosticDescriptor SerializeNonFinite = new() {
		Code = "quirk.serialize.nonFinite",
		Class = DiagnosticClass.Quirk,
		Severity = DiagnosticSeverity.Error,
		Message = "Projection state containing NaN or Infinity throws during serialization (JSON has no representation for non-finite numbers).",
		Docs = "The KurrentDB engine throws when projection state contains `NaN` or `Infinity` (JSON has no representation for them). Guard non-finite numbers in your handler, e.g. store `null` or `0`.",
		FixedIn = new KurrentDbVersion(26, 2, 0), // PR #5610, shipped 26.2.0
	};

	/// <summary>
	/// A bare string returned as (uni-state) projection state is persisted un-encoded - the engine
	/// writes the raw string rather than JSON-encoding it - so the projection faults on reload when
	/// <c>Load()</c> runs <c>JsonParser.Parse</c> on the stored value. Fixed upstream in PR #5610
	/// (26.2.0) by always JSON-encoding. Bi-state state-array slots were never affected; they
	/// always JSON-encode.
	/// </summary>
	public static readonly DiagnosticDescriptor SerializeRawString = new() {
		Code = "quirk.serialize.rawString",
		Class = DiagnosticClass.Quirk,
		Severity = DiagnosticSeverity.Error,
		Message = "A bare string returned as projection state is persisted un-encoded, so the projection faults when it reloads (Load can't JSON-parse the raw string). Wrap the string in an object.",
		Docs = "When a projection returns a bare string as its state (not wrapped in an object), the KurrentDB engine persists it un-encoded - `\"hello\"` is written as `hello` rather than `\"hello\"`. On the next reload (restart, re-enable, resume) `Load()` runs `JSON.parse` on the stored value and throws, so the projection won't resume. Wrap string state in an object (e.g. `{ value: \"hello\" }`), or use KurrentDB 26.2.0+ where the engine JSON-encodes string state. (Bi-state state-array slots are unaffected - they always JSON-encode.)",
		FixedIn = new KurrentDbVersion(26, 2, 0), // PR #5610, shipped 26.2.0
	};

	/// <summary>
	/// Bi-state / <c>$initShared</c> projections are not supported under engine_version 2: the
	/// engine silently re-initializes shared state from <c>$initShared</c> on restart instead of
	/// restoring it from the state stream, producing incorrect results with no error. Detected at
	/// compile time off the resolved definition (gaffer can't reproduce the restart). Not yet fixed
	/// upstream, so <c>FixedIn</c> is null; set it when V2 gains shared-state restore.
	/// </summary>
	public static readonly DiagnosticDescriptor BiStateSharedStateResetOnV2 = new() {
		Code = "quirk.biState.sharedStateResetOnV2",
		Class = DiagnosticClass.Quirk,
		Severity = DiagnosticSeverity.Error,
		Message = "Bi-state / $initShared is not supported under engine_version 2: shared state is silently re-initialized on restart, producing incorrect results. Use engine_version 1.",
		Docs = "Bi-state projections (those declaring `$initShared`, operating on a `[partitionState, sharedState]` pair) are not supported under engine_version 2. The shared-state slot is not restored on restart: after a node restart, projection re-enable, or resume, the engine re-runs `$initShared` instead of reading the persisted shared state, silently producing incorrect results. Use engine_version 1 until KurrentDB implements shared-state restore on V2.",
		FixedIn = null, // not yet supported on V2; set when shared-state restore ships
	};

	/// <summary>
	/// <c>outputState()</c> has no effect under engine_version 2: V2 does not emit result-stream
	/// events, so a projection relying on result-stream subscriptions silently produces nothing.
	/// Result-stream parity is planned for a future release, so <c>FixedIn</c> is null until then.
	/// </summary>
	public static readonly DiagnosticDescriptor OutputStateNoEffectOnV2 = new() {
		Code = "quirk.outputState.noEffectOnV2",
		Class = DiagnosticClass.Quirk,
		Severity = DiagnosticSeverity.Warning,
		Message = "outputState() has no effect under engine_version 2: result streams are not emitted; state is written to the state stream and must be polled.",
		Docs = "`outputState()` has no effect under engine_version 2. V2 does not emit `Result` events to a result stream - state is written only to the `$projections-{name}[-{partition}]-state` stream and must be polled (or that stream subscribed to). Live result-stream parity is planned for a future release; use engine_version 1 until then if you rely on result-stream subscriptions.",
		FixedIn = null, // result-stream parity planned for a future V2 release
	};

	// ---------- usage.* : the user's own projection code ----------

	/// <summary><c>linkStreamTo</c> is undocumented in KurrentDB and may be removed.</summary>
	public static readonly DiagnosticDescriptor LinkStreamToDeprecated = new() {
		Code = "usage.linkStreamTo.deprecated",
		Class = DiagnosticClass.Usage,
		Severity = DiagnosticSeverity.Information,
		Message = "linkStreamTo is undocumented in KurrentDB and may be removed in a future version.",
		Docs = "`linkStreamTo` is undocumented in KurrentDB and may be removed in a future version. Prefer `linkTo`.",
	};

	/// <summary><c>transformBy</c>/<c>filterBy</c> are not invoked under engine_version 2.</summary>
	public static readonly DiagnosticDescriptor TransformsNotInvoked = new() {
		Code = "usage.transforms.notInvoked",
		Class = DiagnosticClass.Usage,
		Severity = DiagnosticSeverity.Warning,
		Message = "transformBy()/filterBy() are registered but never invoked under engine_version=2; result equals post-handler state.",
		Docs = "`transformBy()`/`filterBy()` are registered but never invoked under engine_version 2 - the result equals the post-handler state. Use engine_version 1 for V1 transform behaviour.",
	};

	/// <summary><c>options()</c> called more than once; last-write-wins.</summary>
	public static readonly DiagnosticDescriptor OptionsDuplicate = new() {
		Code = "usage.options.duplicate",
		Class = DiagnosticClass.Usage,
		Severity = DiagnosticSeverity.Information,
		Message = "options() is called more than once; only the last call takes effect and the earlier ones are discarded.",
		Docs = "`options()` is called more than once; only the last call takes effect and earlier ones are discarded. Merge them into a single call.",
	};

	/// <summary><c>reorderEvents</c>/<c>processingLag</c> are a no-op under engine_version 2.</summary>
	public static readonly DiagnosticDescriptor ReorderEventsNoEffectOnV2 = new() {
		Code = "usage.reorderEvents.noEffectOnV2",
		Class = DiagnosticClass.Usage,
		Severity = DiagnosticSeverity.Warning,
		Message = "reorderEvents/processingLag have no effect under engine_version=2; events are processed in arrival order.",
		Docs = "`reorderEvents`/`processingLag` have no effect under engine_version 2 - events are processed in arrival order. Use engine_version 1 if you need event reordering.",
	};

	/// <summary>An <c>async</c> handler silently produces empty state.</summary>
	public static readonly DiagnosticDescriptor HandlerAsync = new() {
		Code = "usage.handler.async",
		Class = DiagnosticClass.Usage,
		Severity = DiagnosticSeverity.Error,
		Message = "async is not supported in a projection handler: the engine runs synchronously, so the returned Promise is serialized as the state (state becomes {}) instead of being awaited.",
		Docs = "`async` handlers are not supported: the projection engine runs synchronously, so the returned Promise is serialized as the state (which becomes `{}`) instead of being awaited. Make the handler synchronous.",
	};

	/// <summary>Returning a Promise from a handler silently produces empty state.</summary>
	public static readonly DiagnosticDescriptor HandlerPromise = new() {
		Code = "usage.handler.promise",
		Class = DiagnosticClass.Usage,
		Severity = DiagnosticSeverity.Error,
		Message = "returning a Promise from a handler is not supported: the engine runs synchronously, so the Promise is serialized as the state (state becomes {}) instead of being awaited.",
		Docs = "Returning a Promise from a handler is not supported: the engine runs synchronously, so the Promise is serialized as the state (which becomes `{}`) instead of being awaited. Return the state synchronously.",
	};

	// Concrete array, iterated by index in the builders below. NativeAOT safety
	// (AGENTS.md): no LINQ extension methods / interface dispatch on arrays in
	// runtime code, since this Sdk compiles into the .so loaded by koffi/Node.
	private static readonly DiagnosticDescriptor[] AllArray = {
		LinkStreamToOutOfBoundsParameters,
		LogMultiParam,
		EventBodyCast,
		SerializeNonFinite,
		SerializeRawString,
		BiStateSharedStateResetOnV2,
		OutputStateNoEffectOnV2,
		LinkStreamToDeprecated,
		TransformsNotInvoked,
		OptionsDuplicate,
		ReorderEventsNoEffectOnV2,
		HandlerAsync,
		HandlerPromise,
	};

	/// <summary>All descriptors, in catalog order. Quirks first, then usage.</summary>
	public static readonly IReadOnlyList<DiagnosticDescriptor> All = AllArray;

	/// <summary>Every reproduced quirk - <see cref="All"/> filtered to <see cref="DiagnosticClass.Quirk"/>.</summary>
	public static readonly IReadOnlyList<DiagnosticDescriptor> Quirks = BuildQuirks();

	private static readonly Dictionary<string, DiagnosticDescriptor> ByCode = BuildByCode();

	/// <summary>Look up a descriptor by its <see cref="DiagnosticDescriptor.Code"/>.</summary>
	public static bool TryGet(string code, out DiagnosticDescriptor descriptor) =>
		ByCode.TryGetValue(code, out descriptor!);

	private static DiagnosticDescriptor[] BuildQuirks() {
		var quirks = new List<DiagnosticDescriptor>(AllArray.Length);
		for (var i = 0; i < AllArray.Length; i++) {
			if (AllArray[i].Class == DiagnosticClass.Quirk)
				quirks.Add(AllArray[i]);
		}
		return quirks.ToArray();
	}

	private static Dictionary<string, DiagnosticDescriptor> BuildByCode() {
		var byCode = new Dictionary<string, DiagnosticDescriptor>(AllArray.Length);
		for (var i = 0; i < AllArray.Length; i++)
			byCode[AllArray[i].Code] = AllArray[i];
		return byCode;
	}
}
