using System.Text.Json;
using Gaffer.Runtime.Errors;
using Gaffer.Sdk.Versioning;

namespace Gaffer.Runtime.Tests;

/// <summary>
/// Pins the JSON wire format that bindings + CLI rely on:
/// - options going in (dbVersion accepted, parsed, validated)
/// - errors coming out (compatCode appears when set)
/// - known-bugs registry export (one entry per KnownBugs.All)
/// </summary>
public class NativeExportsWireFormatTests {
	// -- ParseOptions: dbVersion --

	[Fact]
	public void ParseOptions_DbVersion_Parses() {
		var opts = NativeExports.ParseOptions("""{"engineVersion":2,"dbVersion":"26.1.0"}""");
		Assert.Equal(new KurrentDbVersion(26, 1, 0), opts.DbVersion);
	}

	[Fact]
	public void ParseOptions_DbVersion_MissingIsUnversioned() {
		var opts = NativeExports.ParseOptions("""{"engineVersion":2}""");
		Assert.Null(opts.DbVersion);
	}

	[Fact]
	public void ParseOptions_DbVersion_ExplicitNullIsUnversioned() {
		var opts = NativeExports.ParseOptions("""{"engineVersion":2,"dbVersion":null}""");
		Assert.Null(opts.DbVersion);
	}

	[Fact]
	public void ParseOptions_DbVersion_EmptyIsUnversioned() {
		var opts = NativeExports.ParseOptions("""{"engineVersion":2,"dbVersion":""}""");
		Assert.Null(opts.DbVersion);
	}

	[Theory]
	[InlineData("26.1")]
	[InlineData("v26.1.0")]
	[InlineData("26.1.0-rc.1")]
	[InlineData("not a version")]
	public void ParseOptions_DbVersion_RejectsMalformed(string s) {
		var ex = Assert.Throws<InvalidArgumentException>(() =>
			NativeExports.ParseOptions($$"""{"engineVersion":2,"dbVersion":"{{s}}"}"""));
		Assert.Equal("dbVersion", ex.Field);
		Assert.Contains(s, ex.Message);
	}

	[Theory]
	[InlineData("""{"engineVersion":2,"dbVersion":26}""")]      // number
	[InlineData("""{"engineVersion":2,"dbVersion":true}""")]    // bool
	[InlineData("""{"engineVersion":2,"dbVersion":[]}""")]      // array
	[InlineData("""{"engineVersion":2,"dbVersion":{}}""")]      // object
	public void ParseOptions_DbVersion_RejectsNonStringTypes(string json) {
		var ex = Assert.Throws<InvalidArgumentException>(() => NativeExports.ParseOptions(json));
		Assert.Equal("dbVersion", ex.Field);
	}

	// -- SerializeProjectionError: compatCode --

	[Fact]
	public void SerializeProjectionError_OmitsCompatCodeWhenNull() {
		var ex = new InvalidArgumentException("test", "field");
		var json = NativeExports.SerializeProjectionError(ex);
		using var doc = JsonDocument.Parse(json);
		Assert.False(doc.RootElement.TryGetProperty("compatCode", out _));
	}

	[Fact]
	public void SerializeProjectionError_EmitsCompatCodeWhenSet() {
		var ex = new InvalidArgumentException("test", "field") {
			CompatCode = KnownBugs.EventBodyCast.Code,
		};
		var json = NativeExports.SerializeProjectionError(ex);
		using var doc = JsonDocument.Parse(json);
		Assert.Equal(KnownBugs.EventBodyCast.Code, doc.RootElement.GetProperty("compatCode").GetString());
	}

	// -- SerializeKnownBugs --

	[Fact]
	public void SerializeKnownBugs_ReturnsOneEntryPerRegistryItem() {
		var json = NativeExports.SerializeKnownBugs();
		using var doc = JsonDocument.Parse(json);
		Assert.Equal(JsonValueKind.Array, doc.RootElement.ValueKind);
		Assert.Equal(KnownBugs.All.Count, doc.RootElement.GetArrayLength());
	}

	[Fact]
	public void SerializeKnownBugs_EachEntryHasCodeAndDescription() {
		var json = NativeExports.SerializeKnownBugs();
		using var doc = JsonDocument.Parse(json);
		foreach (var entry in doc.RootElement.EnumerateArray()) {
			Assert.True(entry.TryGetProperty("code", out var code));
			Assert.False(string.IsNullOrEmpty(code.GetString()));
			Assert.True(entry.TryGetProperty("description", out var desc));
			Assert.False(string.IsNullOrEmpty(desc.GetString()));
		}
	}

	[Fact]
	public void SerializeKnownBugs_OmitsFixedInWhenNull() {
		// Today every entry has FixedIn = null (no upstream fix shipped).
		// All entries should omit the field.
		var json = NativeExports.SerializeKnownBugs();
		using var doc = JsonDocument.Parse(json);
		foreach (var entry in doc.RootElement.EnumerateArray()) {
			Assert.False(entry.TryGetProperty("fixedIn", out _));
		}
	}

	[Fact]
	public void SerializeKnownBugs_IncludesAllRegistryCodes() {
		var json = NativeExports.SerializeKnownBugs();
		using var doc = JsonDocument.Parse(json);
		var codes = doc.RootElement.EnumerateArray()
			.Select(e => e.GetProperty("code").GetString())
			.ToHashSet();
		foreach (var bug in KnownBugs.All) {
			Assert.Contains(bug.Code, codes);
		}
	}
}
