using System.Text.Json.Serialization;

namespace Gaffer.Sdk;

[JsonSourceGenerationOptions(PropertyNamingPolicy = JsonKnownNamingPolicy.CamelCase)]
[JsonSerializable(typeof(ProjectionInfo))]
public partial class SdkJsonContext : JsonSerializerContext { }
