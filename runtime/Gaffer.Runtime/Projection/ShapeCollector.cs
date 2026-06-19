using Acornima;
using Acornima.Ast;
using Gaffer.Sdk;

namespace Gaffer.Runtime.Projection;

/// <summary>
/// Walks a projection's AST to produce a <see cref="ProjectionShape"/>:
/// handler kinds, distinct event-name count, and per-builtin call
/// counts. Best-effort - a parse failure produces <c>Parsable=false</c>
/// and otherwise-zero counts; the consumer treats the data as partial.
/// <para>
/// Shares the parsed AST with <see cref="Diagnostics.DiagnosticCollector"/> -
/// both passes run against the single <c>ParseScript</c> result so
/// the cost over the diagnostic-only path is one extra visitor pass
/// (microsecond-scale on typical projections), not a re-parse. Gated
/// by <c>IncludeShape</c> at the FFI: LSP requests diagnostics only
/// and pays nothing for the shape walk.
/// </para>
/// <para>
/// No source content (event names, stream names, projection text)
/// crosses the FFI - the walker emits only call counts and the three
/// <c>$</c>-prefixed handler flags. Plain event names are summed
/// into <see cref="ProjectionShapeHandlers.DistinctEventNames"/> and
/// the names themselves discarded.
/// </para>
/// </summary>
internal static class ShapeCollector {
	// Projection builtins called as bare identifiers: the source-
	// definition entries (`fromAll()`, `fromStream(...)`, ...) plus the
	// in-handler write sinks, which are shared with EmitDetector as the
	// single source of truth for sink names. Caller-position match:
	// CallExpression.Callee is an Identifier with this name.
	// `someService.emit(...)` does NOT count (Identifier-only
	// dispatch).
	private static readonly HashSet<string> IdentifierBuiltins = new(EmitDetector.WriteSinks) {
		"fromAll", "fromStream", "fromStreams", "fromCategory", "fromCategories",
	};

	// Chained projection builtins: methods called on the
	// source-definition pipeline (`.when({...}).foreachStream()...`).
	// Caller-position match: CallExpression.Callee is a non-computed
	// MemberExpression whose property is an Identifier with this
	// name. A single source can call the same chained builtin many
	// times (e.g. transformBy().transformBy()); each call is counted.
	private static readonly HashSet<string> ChainedBuiltins = new() {
		"when", "foreachStream", "outputState", "transformBy", "partitionBy",
		"chainHandlers", "updateOf",
	};

	// Handler kind keys inside `when({...})` and `chainHandlers({...})`
	// object-literal arguments. `$initShared` is treated as `$init` for
	// the Handlers.Init bool (both register state-initialiser handlers;
	// the wire distinction isn't worth a separate flag).
	private const string AnyKey = "$any";
	private const string InitKey = "$init";
	private const string InitSharedKey = "$initShared";
	private const string DeletedKey = "$deleted";

	/// <summary>
	/// Walks <paramref name="ast"/> producing a populated
	/// <see cref="ProjectionShape"/>. <paramref name="fileSize"/> is
	/// the raw byte count of the source - the Go side buckets it
	/// before emit, so callers pass the unrounded value.
	/// <paramref name="parsable"/> reflects whether the source parsed
	/// cleanly; pass <c>false</c> when the AST came from a partial
	/// or recovered parse.
	/// </summary>
	public static ProjectionShape Walk(Script ast, int fileSize, bool parsable) {
		var scanner = new Scanner();
		scanner.Visit(ast);

		return new ProjectionShape {
			Parsable = parsable,
			FileSize = fileSize,
			Handlers = new ProjectionShapeHandlers {
				Any = scanner.AnyHandler,
				Init = scanner.InitHandler,
				Deleted = scanner.DeletedHandler,
				DistinctEventNames = scanner.DistinctEventNames.Count,
			},
			BuiltinCounts = BuildBuiltinCounts(scanner.BuiltinCounts),
		};
	}

	// Translate the scanner's flat dictionary into the typed bag.
	// Absent (count == 0) keys stay null on the C# side so the JSON
	// serialiser omits them entirely.
	private static ProjectionShapeBuiltinCounts BuildBuiltinCounts(Dictionary<string, int> counts) {
		int? Get(string key) => counts.TryGetValue(key, out var n) && n > 0 ? n : null;
		return new ProjectionShapeBuiltinCounts {
			FromAll = Get("fromAll"),
			FromStream = Get("fromStream"),
			FromStreams = Get("fromStreams"),
			FromCategory = Get("fromCategory"),
			FromCategories = Get("fromCategories"),
			When = Get("when"),
			ForeachStream = Get("foreachStream"),
			OutputState = Get("outputState"),
			TransformBy = Get("transformBy"),
			PartitionBy = Get("partitionBy"),
			Emit = Get("emit"),
			LinkTo = Get("linkTo"),
			CopyTo = Get("copyTo"),
			LinkStreamTo = Get("linkStreamTo"),
			ChainHandlers = Get("chainHandlers"),
			UpdateOf = Get("updateOf"),
		};
	}

	private sealed class Scanner : AstVisitor {
		public bool AnyHandler { get; private set; }
		public bool InitHandler { get; private set; }
		public bool DeletedHandler { get; private set; }
		public HashSet<string> DistinctEventNames { get; } = new(StringComparer.Ordinal);
		public Dictionary<string, int> BuiltinCounts { get; } = new();

		protected override object? VisitCallExpression(CallExpression node) {
			// Position-typed dispatch: TopLevelBuiltins only match an
			// Identifier callee (`fromAll()`); ChainedBuiltins only
			// match a MemberExpression callee (`chain.when(...)`).
			// Without this split, `myService.fromAll()` would bump
			// FromAll and `someObj.emit("event")` would bump Emit -
			// both legitimate user-code patterns that should NOT
			// count as projection-builtin calls. Computed access
			// (`obj["when"]()`) is intentionally never matched - we
			// stay static-only.
			switch (node.Callee) {
				case Identifier id when IdentifierBuiltins.Contains(id.Name):
					Bump(id.Name);
					break;
				case MemberExpression { Computed: false, Property: Identifier prop } when ChainedBuiltins.Contains(prop.Name):
					Bump(prop.Name);
					// `when` and `chainHandlers` take an object literal of
					// handlers; harvest the handler kinds + event names.
					if ((prop.Name == "when" || prop.Name == "chainHandlers") && node.Arguments.Count > 0) {
						TryHarvestHandlers(node.Arguments[0]);
					}
					break;
			}
			return base.VisitCallExpression(node);
		}

		private void Bump(string name) {
			BuiltinCounts[name] = BuiltinCounts.GetValueOrDefault(name) + 1;
		}

		private void TryHarvestHandlers(Node arg) {
			if (arg is not ObjectExpression obj) {
				return;
			}
			foreach (var p in obj.Properties) {
				if (p is not Property prop) {
					continue;
				}
				var key = PropertyKeyName(prop);
				if (key is null) {
					continue;
				}
				switch (key) {
					case AnyKey:
						AnyHandler = true;
						break;
					case InitKey:
					case InitSharedKey:
						InitHandler = true;
						break;
					case DeletedKey:
						DeletedHandler = true;
						break;
					default:
						// Plain event-name handler; we only care about
						// the count of distinct names, never the names
						// themselves. Ignore $-prefixed strangers
						// (future spec extensions) so the count
						// reflects real domain events.
						//
						// When a future projection-engine version adds
						// a new $-prefixed handler kind (e.g. $snapshot),
						// add a case above + a matching bool on
						// ProjectionShapeHandlers + a corresponding
						// field in the CUE schema. The "drop unknown
						// $-prefixed keys" default arm is the
						// future-proofing rail.
						if (!key.StartsWith('$')) {
							DistinctEventNames.Add(key);
						}
						break;
				}
			}
		}

		// when({}) properties take three syntactic forms:
		//   `$any: fn`            -> Property.Key = Identifier("$any")
		//   `"$any": fn`          -> Property.Key = StringLiteral("$any")
		//   `[expr]: fn`          -> Computed=true; we ignore
		// Numeric / computed keys are intentionally dropped (projections
		// don't use them for handler names; if they do, we can't tell
		// what they mean without evaluating the expression).
		private static string? PropertyKeyName(Property prop) {
			if (prop.Computed) {
				return null;
			}
			return prop.Key switch {
				Identifier id => id.Name,
				StringLiteral lit => lit.Value,
				_ => null,
			};
		}
	}
}
