using Gaffer.Runtime.Projection;
using Gaffer.Sdk;
using Gaffer.Sdk.Diagnostics;

namespace Gaffer.Runtime.Tests;

public class ProjectionInfoMapperTests {
	[Fact]
	public void ToProjectionInfo_MapsAllFields() {
		var sources = new QuerySources {
			AllStreams = true,
			AllEvents = true,
			Categories = ["orders", "carts"],
			Streams = ["s-1", "s-2"],
			Events = ["Placed", "Shipped"],
			ByStreams = true,
			ByCustomPartitions = true,
			IsBiState = true,
			DefinesFold = true,
			DefinesStateTransform = true,
			ProducesResults = true,
			HandlesDeletedNotifications = true,
			IncludeLinks = true,
			ResultStreamName = "result-stream",
			PartitionResultStreamNamePattern = "result-{0}",
			ReorderEvents = true,
			ProcessingLag = 250,
		};

		var info = ProjectionInfoMapper.ToProjectionInfo(sources);

		Assert.True(info.AllStreams);
		Assert.True(info.AllEvents);
		Assert.Equal(["orders", "carts"], info.Categories!);
		Assert.Equal(["s-1", "s-2"], info.Streams!);
		Assert.Equal(["Placed", "Shipped"], info.Events!);
		Assert.True(info.ByStreams);
		Assert.True(info.ByCustomPartitions);
		Assert.True(info.BiState);
		Assert.True(info.DefinesHandlers);
		Assert.True(info.DefinesStateTransform);
		Assert.True(info.ProducesResults);
		Assert.True(info.HandlesDeletedNotifications);
		Assert.True(info.IncludeLinks);
		Assert.Equal("result-stream", info.ResultStreamName);
		Assert.Equal("result-{0}", info.PartitionResultStreamNamePattern);
		Assert.True(info.ReorderEvents);
		Assert.Equal(250, info.ProcessingLag);
	}

	[Fact]
	public void ToProjectionInfo_PreservesNullsAndDefaults() {
		var sources = new QuerySources();

		var info = ProjectionInfoMapper.ToProjectionInfo(sources);

		Assert.False(info.AllStreams);
		Assert.False(info.AllEvents);
		Assert.Null(info.Categories);
		Assert.Null(info.Streams);
		Assert.Null(info.Events);
		Assert.False(info.BiState);
		Assert.False(info.DefinesHandlers);
		Assert.Null(info.ResultStreamName);
		Assert.Null(info.PartitionResultStreamNamePattern);
		Assert.Null(info.ProcessingLag);
		Assert.Null(info.Diagnostics);
	}

	[Fact]
	public void ToProjectionInfo_PassesDiagnosticsThrough() {
		var diagnostics = new[] {
			new Diagnostic {
				Code = "usage.linkStreamTo.deprecated",
				Message = "linkStreamTo is undocumented in KurrentDB and may be removed.",
				Severity = DiagnosticSeverity.Warning,
				Range = new SourceRange {
					Start = new SourcePosition { Line = 7, Column = 3 },
					End = new SourcePosition { Line = 7, Column = 16 },
				},
			},
		};

		var info = ProjectionInfoMapper.ToProjectionInfo(new QuerySources(), diagnostics);

		Assert.Same(diagnostics, info.Diagnostics);
	}

	[Fact]
	public void Serialize_UsesCamelCase() {
		var info = ProjectionInfoMapper.ToProjectionInfo(new QuerySources {
			AllStreams = true,
			IsBiState = true,
			DefinesFold = true,
		});

		var json = System.Text.Json.JsonSerializer.Serialize(
			info, Sdk.SdkJsonContext.Default.ProjectionInfo);

		Assert.Contains("\"allStreams\":true", json);
		Assert.Contains("\"biState\":true", json);
		Assert.Contains("\"definesHandlers\":true", json);
		Assert.DoesNotContain("\"AllStreams\"", json);
		Assert.DoesNotContain("\"IsBiState\"", json);
		Assert.DoesNotContain("\"DefinesFold\"", json);
	}

	[Fact]
	public void Serialize_DiagnosticRoundTrip() {
		var diagnostics = new[] {
			new Diagnostic {
				Code = "usage.linkStreamTo.deprecated",
				Message = "linkStreamTo is undocumented in KurrentDB and may be removed.",
				Severity = DiagnosticSeverity.Warning,
				Range = new SourceRange {
					Start = new SourcePosition { Line = 12, Column = 5 },
					End = new SourcePosition { Line = 12, Column = 18 },
				},
			},
		};
		var info = ProjectionInfoMapper.ToProjectionInfo(new QuerySources(), diagnostics);

		var json = System.Text.Json.JsonSerializer.Serialize(
			info, Sdk.SdkJsonContext.Default.ProjectionInfo);

		var decoded = System.Text.Json.JsonSerializer.Deserialize(
			json, Sdk.SdkJsonContext.Default.ProjectionInfo);
		Assert.NotNull(decoded);
		Assert.NotNull(decoded!.Diagnostics);
		Assert.Single(decoded.Diagnostics!);
		var d = decoded.Diagnostics![0];
		Assert.Equal("usage.linkStreamTo.deprecated", d.Code);
		Assert.Equal(DiagnosticSeverity.Warning, d.Severity);
		Assert.NotNull(d.Range);
		Assert.Equal(12, d.Range!.Start.Line);
		Assert.Equal(5, d.Range.Start.Column);
		Assert.Equal(12, d.Range.End.Line);
		Assert.Equal(18, d.Range.End.Column);
	}

	[Fact]
	public void PassesShapeThrough() {
		// New optional `shape` parameter passes through verbatim,
		// mirroring the diagnostics passthrough. Without this test
		// a future reorder / default-flip of the optional args
		// could silently break the wire.
		var shape = new Sdk.ProjectionShape {
			Parsable = true,
			FileSize = 256,
			Handlers = new Sdk.ProjectionShapeHandlers { Any = true },
		};

		var info = ProjectionInfoMapper.ToProjectionInfo(new QuerySources(), diagnostics: null, shape: shape);

		Assert.Same(shape, info.Shape);
	}

	[Fact]
	public void ShapeDefaultsNull() {
		// Backward-compat: callers that omit the new parameter
		// still get Shape=null (LSP / non-telemetry path).
		var info = ProjectionInfoMapper.ToProjectionInfo(new QuerySources());

		Assert.Null(info.Shape);
	}
}
