using System.Runtime.CompilerServices;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Serialization;
using Gaffer.Sdk.Diagnostics;

namespace Gaffer.Runtime.Tests;

/// <summary>
/// Generates and drift-guards the diagnostics catalogue artifact the CLI embeds and the MCP
/// server serves. The C# <see cref="DiagnosticCatalog"/> is the single source of truth; this
/// test renders the JSON and, on drift, rewrites the committed file and fails (the same
/// promote-on-mismatch ergonomic as the Verify snapshots). Keeping the renderer in the test
/// (rather than the AOT-published Sdk) keeps reflection-based JSON out of the native build.
/// </summary>
public class DiagnosticsArtifactTests {
	// Committed artifact, consumed by cli/internal/mcpserver. Path resolved from this source
	// file so it's independent of the test runner's working directory (the telemetry codegen
	// writes cross-module into the cli tree the same way).
	private static string ArtifactPath([CallerFilePath] string thisFile = "") {
		var repoRoot = Path.GetFullPath(Path.Combine(Path.GetDirectoryName(thisFile)!, "..", ".."));
		return Path.Combine(repoRoot, "cli", "internal", "mcpserver", "resources", "diagnostics.gen.json");
	}

	private sealed record DiagnosticDoc {
		[JsonPropertyName("code")] public required string Code { get; init; }
		[JsonPropertyName("class")] public required string Class { get; init; }
		[JsonPropertyName("severity")] public required string Severity { get; init; }
		[JsonPropertyName("message")] public required string Message { get; init; }
		[JsonPropertyName("docs")] public string? Docs { get; init; }
		[JsonPropertyName("fixedIn")] public string? FixedIn { get; init; }
		[JsonPropertyName("badExample")] public string? BadExample { get; init; }
		[JsonPropertyName("goodExample")] public string? GoodExample { get; init; }
	}

	private static readonly JsonSerializerOptions Options = new() {
		WriteIndented = true,
		DefaultIgnoreCondition = JsonIgnoreCondition.Never,
		// Keep backticks, +, and quotes literal so the committed artifact is readable.
		Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping,
	};

	private static string Render() {
		var docs = DiagnosticCatalog.All.Select(d => new DiagnosticDoc {
			Code = d.Code,
			Class = d.Class.ToString().ToLowerInvariant(),
			Severity = d.Severity.ToString().ToLowerInvariant(),
			Message = d.Message,
			Docs = d.Docs,
			FixedIn = d.FixedIn?.ToString(),
			BadExample = d.BadExample,
			GoodExample = d.GoodExample,
		}).ToList();
		// Trailing newline so the committed file is POSIX-clean.
		return JsonSerializer.Serialize(docs, Options) + "\n";
	}

	[Fact]
	public void DiagnosticsArtifact_IsUpToDate() {
		var path = ArtifactPath();
		var rendered = Render();
		var committed = File.Exists(path) ? File.ReadAllText(path) : null;
		if (committed == rendered)
			return;

		File.WriteAllText(path, rendered);
		Assert.Fail(
			$"diagnostics.gen.json was stale and has been regenerated from DiagnosticCatalog. " +
			$"Commit the updated file: {path}");
	}
}
