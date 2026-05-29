using Gaffer.Sdk.Versioning;

namespace Gaffer.Runtime.Tests;

public class KnownQuirksTests {
	[Fact]
	public void All_ContainsEveryDeclaredQuirk() {
		Assert.Contains(KnownQuirks.LinkStreamToOutOfBoundsParameters, KnownQuirks.All);
		Assert.Contains(KnownQuirks.LogMultiParam, KnownQuirks.All);
		Assert.Contains(KnownQuirks.EventBodyCast, KnownQuirks.All);
		Assert.Contains(KnownQuirks.BiStateStringSlot, KnownQuirks.All);
		Assert.Contains(KnownQuirks.SerializeNonFinite, KnownQuirks.All);
	}

	[Fact]
	public void All_CodesAreUniqueAndCompatNamespaced() {
		var codes = KnownQuirks.All.Select(b => b.Code).ToList();
		Assert.Equal(codes.Count, codes.Distinct().Count());
		Assert.All(codes, c => Assert.StartsWith("compat.", c));
	}

	[Fact]
	public void Quirk_FiresWhenUnversioned() {
		// Default unversioned = "all quirks on" - every known quirk fires.
		Assert.All(KnownQuirks.All, b => Assert.True(b.FiresAt(null)));
	}

	[Fact]
	public void AlwaysQuirky_FixedInIsNull() {
		Assert.Null(KnownQuirks.LinkStreamToOutOfBoundsParameters.FixedIn);
		Assert.Null(KnownQuirks.LogMultiParam.FixedIn);
	}

	[Fact]
	public void GatedWithKnownFix_DoesNotFireAtOrAboveFixVersion() {
		// Synthetic check using a constructed quirk, since real PR #5610 isn't
		// merged yet and all current entries have FixedIn = null. This locks
		// the FiresAt semantics so the eventual flip behaves correctly.
		var fixedAt = new KurrentDbVersion(26, 1, 1);
		var quirk = new Quirk { Code = "compat.test.synthetic", Description = "test", FixedIn = fixedAt };

		Assert.True(quirk.FiresAt(null));
		Assert.True(quirk.FiresAt(new KurrentDbVersion(26, 0, 0)));
		Assert.True(quirk.FiresAt(new KurrentDbVersion(26, 1, 0)));
		Assert.False(quirk.FiresAt(new KurrentDbVersion(26, 1, 1)));
		Assert.False(quirk.FiresAt(new KurrentDbVersion(27, 0, 0)));
	}
}
