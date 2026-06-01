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
		HasRuntimeBehaviour = true,
		FixedIn = null, // no upstream PR in flight
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
		HasRuntimeBehaviour = true,
		FixedIn = null, // no upstream PR
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
		HasRuntimeBehaviour = true,
		FixedIn = null, // PR #5610 open, expected 26.1.1
	};

	/// <summary>
	/// BiState <c>PrepareOutput</c> JSON-quotes a raw string in slot 0 of the state array:
	/// upstream checks <c>_state.IsString()</c> (the array) instead of <c>state.IsString()</c>
	/// (the element), so the passthrough branch is unreachable. Fixed upstream in PR #5610.
	/// </summary>
	public static readonly DiagnosticDescriptor BiStateStringSlot = new() {
		Code = "quirk.biState.stringSlot",
		Class = DiagnosticClass.Quirk,
		Severity = DiagnosticSeverity.Warning,
		Message = "A raw string in the bi-state partition slot is JSON-quoted instead of passed through, due to an upstream bug.",
		Docs = "In a bi-state projection, a raw string in the partition-state slot is JSON-quoted (double-encoded) when persisted by the KurrentDB engine rather than passed through. Wrap string state in an object to avoid the double-encoding.",
		HasRuntimeBehaviour = true,
		FixedIn = null, // PR #5610
	};

	/// <summary>
	/// BiState <c>PrepareOutput</c> JSON-quotes a raw string in slot 1 (shared state). Unlike
	/// slot 0 there is no passthrough branch at all, and PR #5610's slot-0 fix does not touch it,
	/// so it has no fix version. Tracked separately from <see cref="BiStateStringSlot"/>.
	/// </summary>
	public static readonly DiagnosticDescriptor BiStateSharedStringSlot = new() {
		Code = "quirk.biState.sharedStringSlot",
		Class = DiagnosticClass.Quirk,
		Severity = DiagnosticSeverity.Warning,
		Message = "A raw string in the bi-state shared slot is JSON-quoted instead of passed through, due to an upstream bug.",
		Docs = "In a bi-state projection, a raw string in the shared-state slot is JSON-quoted (double-encoded) when persisted by the KurrentDB engine. Wrap shared string state in an object to avoid the double-encoding.",
		HasRuntimeBehaviour = true,
		FixedIn = null, // no upstream fix; PR #5610 only fixed slot 0
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
		HasRuntimeBehaviour = true,
		FixedIn = null, // PR #5610
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

	/// <summary><c>outputState()</c> is a no-op under engine_version 2.</summary>
	public static readonly DiagnosticDescriptor OutputStateUnconditional = new() {
		Code = "usage.outputState.unconditional",
		Class = DiagnosticClass.Usage,
		Severity = DiagnosticSeverity.Information,
		Message = "outputState() has no effect under engine_version=2; state is always emitted to the result stream.",
		Docs = "`outputState()` has no effect under engine_version 2 - state is always emitted to the result stream. The call is redundant.",
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

	/// <summary>All descriptors, in catalog order. Quirks first, then usage.</summary>
	public static readonly IReadOnlyList<DiagnosticDescriptor> All = new[] {
		LinkStreamToOutOfBoundsParameters,
		LogMultiParam,
		EventBodyCast,
		BiStateStringSlot,
		BiStateSharedStringSlot,
		SerializeNonFinite,
		LinkStreamToDeprecated,
		TransformsNotInvoked,
		OutputStateUnconditional,
		OptionsDuplicate,
		ReorderEventsNoEffectOnV2,
		HandlerAsync,
		HandlerPromise,
	};

	/// <summary>Every reproduced quirk - <see cref="All"/> filtered to <see cref="DiagnosticClass.Quirk"/>.</summary>
	public static readonly IReadOnlyList<DiagnosticDescriptor> Quirks =
		All.Where(d => d.Class == DiagnosticClass.Quirk).ToArray();
}
