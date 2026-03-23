using System.Text.Json.Serialization;
using Gaffer.Runtime.Projection;

namespace Gaffer.Runtime;

[JsonSerializable(typeof(QuerySources))]
[JsonSerializable(typeof(Dictionary<string, string?>))]
internal partial class GafferJsonContext : JsonSerializerContext { }
