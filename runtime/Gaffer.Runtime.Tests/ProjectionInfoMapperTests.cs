using Gaffer.Runtime.Projection;

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
}
