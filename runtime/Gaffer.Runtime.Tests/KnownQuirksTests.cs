using Gaffer.Sdk.Diagnostics;
using Gaffer.Sdk.Versioning;

namespace Gaffer.Runtime.Tests;

public class KnownQuirksTests {
	[Fact]
	public void All_ContainsEveryDeclaredQuirk() {
		Assert.Contains(DiagnosticCatalog.LinkStreamToOutOfBoundsParameters, DiagnosticCatalog.Quirks);
		Assert.Contains(DiagnosticCatalog.LogMultiParam, DiagnosticCatalog.Quirks);
		Assert.Contains(DiagnosticCatalog.EventBodyCast, DiagnosticCatalog.Quirks);
		Assert.Contains(DiagnosticCatalog.BiStateStringSlot, DiagnosticCatalog.Quirks);
		Assert.Contains(DiagnosticCatalog.SerializeNonFinite, DiagnosticCatalog.Quirks);
	}

	[Fact]
	public void All_CodesAreUniqueAndQuirkNamespaced() {
		var codes = DiagnosticCatalog.Quirks.Select(b => b.Code).ToList();
		Assert.Equal(codes.Count, codes.Distinct().Count());
		Assert.All(codes, c => Assert.StartsWith("quirk.", c));
	}

	[Fact]
	public void Quirk_FiresWhenUnversioned() {
		// Default unversioned = "all quirks on" - every known quirk fires.
		Assert.All(DiagnosticCatalog.Quirks, b => Assert.True(b.FiresAt(null)));
	}

	[Fact]
	public void AlwaysQuirky_FixedInIsNull() {
		Assert.Null(DiagnosticCatalog.LinkStreamToOutOfBoundsParameters.FixedIn);
		Assert.Null(DiagnosticCatalog.LogMultiParam.FixedIn);
	}

	[Fact]
	public void GatedWithKnownFix_DoesNotFireAtOrAboveFixVersion() {
		// Synthetic check using a constructed quirk, since real PR #5610 isn't
		// merged yet and all current entries have FixedIn = null. This locks
		// the FiresAt semantics so the eventual flip behaves correctly.
		var fixedAt = new KurrentDbVersion(26, 1, 1);
		var quirk = new DiagnosticDescriptor {
			Code = "quirk.test.synthetic",
			Class = DiagnosticClass.Quirk,
			Severity = DiagnosticSeverity.Warning,
			Message = "test",
			FixedIn = fixedAt,
		};

		Assert.True(quirk.FiresAt(null));
		Assert.True(quirk.FiresAt(new KurrentDbVersion(26, 0, 0)));
		Assert.True(quirk.FiresAt(new KurrentDbVersion(26, 1, 0)));
		Assert.False(quirk.FiresAt(new KurrentDbVersion(26, 1, 1)));
		Assert.False(quirk.FiresAt(new KurrentDbVersion(27, 0, 0)));
	}
}
