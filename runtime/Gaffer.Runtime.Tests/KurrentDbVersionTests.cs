using Gaffer.Sdk.Versioning;

namespace Gaffer.Runtime.Tests;

public class KurrentDbVersionTests {
	[Theory]
	[InlineData("26.1.0", 26, 1, 0)]
	[InlineData("0.0.0", 0, 0, 0)]
	[InlineData("100.99.999", 100, 99, 999)]
	public void Parse_AcceptsValidThreeComponent(string s, int maj, int min, int patch) {
		var v = KurrentDbVersion.Parse(s);
		Assert.Equal(maj, v.Major);
		Assert.Equal(min, v.Minor);
		Assert.Equal(patch, v.Patch);
	}

	[Theory]
	[InlineData("")]
	[InlineData("26")]
	[InlineData("26.1")]
	[InlineData("26.1.0.0")]
	[InlineData("26.1.0-rc.1")]
	[InlineData("v26.1.0")]
	[InlineData("a.b.c")]
	[InlineData("26.1.x")]
	[InlineData("-1.0.0")]
	public void Parse_RejectsInvalid(string s) {
		Assert.False(KurrentDbVersion.TryParse(s, out _));
		Assert.Throws<FormatException>(() => KurrentDbVersion.Parse(s));
	}

	[Fact]
	public void ToString_RoundTrips() {
		var v = new KurrentDbVersion(26, 1, 0);
		Assert.Equal("26.1.0", v.ToString());
		Assert.Equal(v, KurrentDbVersion.Parse(v.ToString()));
	}

	[Fact]
	public void Comparison_OrdersMajorMinorPatch() {
		var a = new KurrentDbVersion(26, 0, 0);
		var b = new KurrentDbVersion(26, 1, 0);
		var c = new KurrentDbVersion(26, 1, 1);
		var d = new KurrentDbVersion(27, 0, 0);

		Assert.True(a < b);
		Assert.True(b < c);
		Assert.True(c < d);
		Assert.True(a < d);
		Assert.False(b < a);
		var bAlso = new KurrentDbVersion(26, 1, 0);
		Assert.True(b >= bAlso);
		Assert.True(b <= bAlso);
	}

	[Fact]
	public void Equality_IsValueBased() {
		Assert.Equal(new KurrentDbVersion(26, 1, 0), new KurrentDbVersion(26, 1, 0));
		Assert.NotEqual(new KurrentDbVersion(26, 1, 0), new KurrentDbVersion(26, 1, 1));
	}
}
