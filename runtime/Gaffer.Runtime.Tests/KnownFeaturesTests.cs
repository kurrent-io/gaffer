using Gaffer.Sdk.Versioning;

namespace Gaffer.Runtime.Tests;

public class KnownFeaturesTests {
	[Fact]
	public void ProjectionsV2_IntroducedIn26_1_0() {
		Assert.Equal(new KurrentDbVersion(26, 1, 0), KnownFeatures.ProjectionsV2.IntroducedIn);
	}

	[Fact]
	public void ProjectionsV2_AvailableAt_PermissiveOnNull() {
		Assert.True(KnownFeatures.ProjectionsV2.AvailableAt(null));
	}

	[Theory]
	[InlineData(25, 0, 0, false)]
	[InlineData(26, 0, 0, false)]
	[InlineData(26, 1, 0, true)]
	[InlineData(26, 1, 1, true)]
	[InlineData(27, 0, 0, true)]
	public void ProjectionsV2_AvailableAt_GateOn26_1_0(int maj, int min, int patch, bool expected) {
		Assert.Equal(expected,
			KnownFeatures.ProjectionsV2.AvailableAt(new KurrentDbVersion(maj, min, patch)));
	}

	[Fact]
	public void All_CodesAreUniqueAndFeatureNamespaced() {
		var codes = KnownFeatures.All.Select(f => f.Code).ToList();
		Assert.Equal(codes.Count, codes.Distinct().Count());
		Assert.All(codes, c => Assert.StartsWith("feature.", c));
	}
}
