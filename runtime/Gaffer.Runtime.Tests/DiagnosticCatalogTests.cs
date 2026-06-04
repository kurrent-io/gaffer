using System.Reflection;
using Gaffer.Sdk.Diagnostics;
using Gaffer.Sdk.Versioning;

namespace Gaffer.Runtime.Tests;

public class DiagnosticCatalogTests {
	[Fact]
	public void All_ContainsEveryDeclaredDescriptor() {
		// DiagnosticCatalog.All is the hand-maintained list everything derives from
		// (diagnostics.gen.json, the MCP resource, TryGet enrichment). Enumerate every
		// declared descriptor field by reflection so a descriptor left out of All fails
		// here rather than silently vanishing from every downstream surface.
		var declared = typeof(DiagnosticCatalog)
			.GetFields(BindingFlags.Public | BindingFlags.Static)
			.Where(f => f.FieldType == typeof(DiagnosticDescriptor))
			.Select(f => (DiagnosticDescriptor)f.GetValue(null)!)
			.ToList();

		Assert.NotEmpty(declared);
		foreach (var d in declared)
			Assert.Contains(d, DiagnosticCatalog.All);
		Assert.Equal(declared.Count, DiagnosticCatalog.All.Count);
	}

	[Fact]
	public void All_CodesAreUniqueAndTaxonomyNamespaced() {
		var codes = DiagnosticCatalog.All.Select(d => d.Code).ToList();
		Assert.Equal(codes.Count, codes.Distinct().Count());
		Assert.All(codes, c => Assert.True(
			c.StartsWith("quirk.") || c.StartsWith("usage."),
			$"code {c} is not quirk.*/usage.*-namespaced"));
	}

	[Fact]
	public void Quirks_AreAllClassQuirk() {
		Assert.All(DiagnosticCatalog.Quirks, d => Assert.Equal(DiagnosticClass.Quirk, d.Class));
		Assert.All(DiagnosticCatalog.Quirks, d => Assert.StartsWith("quirk.", d.Code));
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
	public void FixedQuirks_CarryUpstreamFixVersion() {
		// PR #5610 fixed these in 26.2.0; FiresAt suppresses them at/above that version.
		var fixedIn = new KurrentDbVersion(26, 2, 0);
		Assert.Equal(fixedIn, DiagnosticCatalog.EventBodyCast.FixedIn);
		Assert.Equal(fixedIn, DiagnosticCatalog.SerializeNonFinite.FixedIn);
		Assert.False(DiagnosticCatalog.EventBodyCast.FiresAt(fixedIn));
		Assert.True(DiagnosticCatalog.EventBodyCast.FiresAt(new KurrentDbVersion(26, 1, 0)));
	}

	[Fact]
	public void TryGet_ResolvesDeclaredCodesAndRejectsUnknown() {
		Assert.True(DiagnosticCatalog.TryGet(DiagnosticCatalog.EventBodyCast.Code, out var found));
		Assert.Equal(DiagnosticCatalog.EventBodyCast, found);
		Assert.False(DiagnosticCatalog.TryGet("quirk.not.real", out _));
	}

	[Fact]
	public void GatedWithKnownFix_DoesNotFireAtOrAboveFixVersion() {
		// Synthetic constructed quirk, locking the FiresAt semantics independent
		// of any catalog entry's FixedIn (bodyCast / nonFinite now carry a real
		// FixedIn from PR #5610; others remain null).
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
