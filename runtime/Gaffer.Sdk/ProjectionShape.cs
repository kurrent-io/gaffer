namespace Gaffer.Sdk;

/// <summary>
/// Structural snapshot of a projection's source. Populated by the
/// Acornima walker in <c>Gaffer.Runtime.Projection.ShapeCollector</c>
/// when the FFI caller requests it via <c>IncludeShape</c>; absent
/// otherwise (the LSP and most other consumers pay zero walker cost).
/// <para>
/// The shape is sent to the gaffer telemetry worker via the
/// <c>projection_shape</c> event so we can analyse which projection
/// patterns appear in the wild. No projection content (event names,
/// stream names, source code) is included - only handler kinds and
/// bucketed call counts.
/// </para>
/// </summary>
public sealed class ProjectionShape {
	/// <summary>
	/// <c>true</c> when the AST parser succeeded. <c>false</c> = parser
	/// error; <see cref="Handlers"/> and <see cref="BuiltinCounts"/>
	/// are best-effort partial.
	/// </summary>
	public bool Parsable { get; init; }

	/// <summary>
	/// Raw file size in bytes. The Go side rounds this to one of
	/// {0, 1KB, 5KB, 20KB, 100KB} at marshal time; we send the raw
	/// count so the bucket policy lives in one place.
	/// </summary>
	public int FileSize { get; init; }

	public ProjectionShapeHandlers Handlers { get; init; } = new();
	public ProjectionShapeBuiltinCounts BuiltinCounts { get; init; } = new();
}

/// <summary>
/// Which handlers the projection registers. Boolean for the
/// <c>$</c>-prefixed kinds; the count of plain event-name handlers
/// is bucketed Go-side.
/// </summary>
public sealed class ProjectionShapeHandlers {
	public bool Any { get; init; }
	public bool Init { get; init; }
	public bool Deleted { get; init; }

	/// <summary>
	/// Raw count of distinct event-name handlers (i.e. those other
	/// than <c>$any</c>, <c>$init</c>, <c>$initShared</c>,
	/// <c>$deleted</c>). Names themselves are never sent.
	/// </summary>
	public int DistinctEventNames { get; init; }
}

/// <summary>
/// Raw call counts per allowlisted projection builtin. The Go side
/// applies the bucket rounding and JSON-omits zero counts so a
/// builtin not called is absent on the wire. .NET-side we store
/// nullable ints so deserialisers can tell "absent" from "zero"
/// (they're the same on the wire after bucketing, but the data
/// model makes the distinction explicit while a value is being
/// accumulated).
/// </summary>
public sealed class ProjectionShapeBuiltinCounts {
	public int? FromAll { get; init; }
	public int? FromStream { get; init; }
	public int? FromStreams { get; init; }
	public int? FromCategory { get; init; }
	public int? FromCategories { get; init; }
	public int? When { get; init; }
	public int? ForeachStream { get; init; }
	public int? OutputState { get; init; }
	public int? TransformBy { get; init; }
	public int? PartitionBy { get; init; }
	public int? Emit { get; init; }
	public int? LinkTo { get; init; }
	public int? CopyTo { get; init; }

	/// <summary>linkStreamTo is deprecated; tracked because deprecated.</summary>
	public int? LinkStreamTo { get; init; }

	public int? ChainHandlers { get; init; }
	public int? UpdateOf { get; init; }
}
