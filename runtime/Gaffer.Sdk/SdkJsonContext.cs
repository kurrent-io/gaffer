using System.Text.Json.Serialization;
using Gaffer.Sdk.Diagnostics;

namespace Gaffer.Sdk;

// Default policy: keep nulls. ProjectionInfo's nullable arrays
// (Categories / Streams / Events / Diagnostics) and nullable
// strings (ResultStreamName / PartitionResultStreamNamePattern)
// serialize as `null` rather than being omitted - the wire
// contract the JS bindings expect.
//
// Fields that want omission instead (ProjectionInfo.Shape when the
// FFI caller didn't request it; ProjectionShapeBuiltinCounts.* when
// a builtin wasn't called) carry per-property [JsonIgnore(Condition
// = WhenWritingNull)] in their own declarations.
[JsonSourceGenerationOptions(
	PropertyNamingPolicy = JsonKnownNamingPolicy.CamelCase)]
[JsonSerializable(typeof(ProjectionInfo))]
[JsonSerializable(typeof(ProjectionShape))]
[JsonSerializable(typeof(Diagnostic[]))]
public partial class SdkJsonContext : JsonSerializerContext { }
