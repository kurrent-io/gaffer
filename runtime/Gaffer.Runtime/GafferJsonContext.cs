using System.Text.Json.Serialization;

namespace Gaffer.Runtime;

[JsonSerializable(typeof(Dictionary<string, string?>))]
internal partial class GafferJsonContext : JsonSerializerContext { }
