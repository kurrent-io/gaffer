using Gaffer.Sdk.Versioning;

namespace Gaffer.Runtime.Tests;

public class KnownBugsTests {
	[Fact]
	public void All_ContainsEveryDeclaredBug() {
		Assert.Contains(KnownBugs.LinkStreamToOutOfBoundsParameters, KnownBugs.All);
		Assert.Contains(KnownBugs.LogMultiParam, KnownBugs.All);
		Assert.Contains(KnownBugs.EventBodyCast, KnownBugs.All);
		Assert.Contains(KnownBugs.BiStateStringSlot, KnownBugs.All);
		Assert.Contains(KnownBugs.SerializeNonFinite, KnownBugs.All);
	}

	[Fact]
	public void All_CodesAreUniqueAndCompatNamespaced() {
		var codes = KnownBugs.All.Select(b => b.Code).ToList();
		Assert.Equal(codes.Count, codes.Distinct().Count());
		Assert.All(codes, c => Assert.StartsWith("compat.", c));
	}

	[Fact]
	public void Bug_FiresWhenUnversioned() {
		// Default unversioned = "all bugs on" - every known bug fires.
		Assert.All(KnownBugs.All, b => Assert.True(b.FiresAt(null)));
	}

	[Fact]
	public void AlwaysBuggy_FixedInIsNull() {
		Assert.Null(KnownBugs.LinkStreamToOutOfBoundsParameters.FixedIn);
		Assert.Null(KnownBugs.LogMultiParam.FixedIn);
	}

	[Fact]
	public void GatedWithKnownFix_DoesNotFireAtOrAboveFixVersion() {
		// Synthetic check using a constructed bug, since real PR #5610 isn't
		// merged yet and all current entries have FixedIn = null. This locks
		// the FiresAt semantics so the eventual flip behaves correctly.
		var fixedAt = new KurrentDbVersion(26, 1, 1);
		var bug = new Bug { Code = "compat.test.synthetic", Description = "test", FixedIn = fixedAt };

		Assert.True(bug.FiresAt(null));
		Assert.True(bug.FiresAt(new KurrentDbVersion(26, 0, 0)));
		Assert.True(bug.FiresAt(new KurrentDbVersion(26, 1, 0)));
		Assert.False(bug.FiresAt(new KurrentDbVersion(26, 1, 1)));
		Assert.False(bug.FiresAt(new KurrentDbVersion(27, 0, 0)));
	}
}
