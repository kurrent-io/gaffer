using System.Text.Json.Serialization;

namespace Gaffer.Sdk;

// WhenWritingNull on the source-gen options means: nullable
// fields with null values are absent on the wire. The
// `projection_shape.builtin_counts` bag relies on this - a builtin
// not called must not appear in the JSON (the Go side decodes the
// `*RawCount` pointer as nil and the worker treats absent and zero
// identically). Also keeps `ProjectionInfo.Shape` absent when the
// FFI caller didn't request it.
[JsonSourceGenerationOptions(
	PropertyNamingPolicy = JsonKnownNamingPolicy.CamelCase,
	DefaultIgnoreCondition = JsonIgnoreCondition.WhenWritingNull)]
[JsonSerializable(typeof(ProjectionInfo))]
[JsonSerializable(typeof(ProjectionShape))]
public partial class SdkJsonContext : JsonSerializerContext { }
