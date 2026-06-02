using System.Text.Json;
using Gaffer.Runtime.Errors;
using Gaffer.Sdk.Diagnostics;
using Gaffer.Sdk.Versioning;

namespace Gaffer.Runtime.Tests;

/// <summary>
/// Pins the JSON wire format that bindings + CLI rely on:
/// - options going in (quirksVersion accepted, parsed, validated)
/// - errors coming out (compatCode appears when set)
/// - known-quirks registry export (one entry per DiagnosticCatalog.Quirks)
/// </summary>
public class NativeExportsWireFormatTests {
	// -- ParseOptions: quirksVersion --

	[Fact]
	public void ParseOptions_QuirksVersion_Parses() {
		var opts = NativeExports.ParseOptions("""{"engineVersion":2,"quirksVersion":"26.1.0"}""");
		Assert.Equal(new KurrentDbVersion(26, 1, 0), opts.QuirksVersion);
	}

	[Fact]
	public void ParseOptions_QuirksVersion_MissingIsUnversioned() {
		var opts = NativeExports.ParseOptions("""{"engineVersion":2}""");
		Assert.Null(opts.QuirksVersion);
	}

	[Fact]
	public void ParseOptions_QuirksVersion_ExplicitNullIsUnversioned() {
		var opts = NativeExports.ParseOptions("""{"engineVersion":2,"quirksVersion":null}""");
		Assert.Null(opts.QuirksVersion);
	}

	[Fact]
	public void ParseOptions_QuirksVersion_EmptyIsUnversioned() {
		var opts = NativeExports.ParseOptions("""{"engineVersion":2,"quirksVersion":""}""");
		Assert.Null(opts.QuirksVersion);
	}

	[Theory]
	[InlineData("26.1")]
	[InlineData("v26.1.0")]
	[InlineData("26.1.0-rc.1")]
	[InlineData("not a version")]
	public void ParseOptions_QuirksVersion_RejectsMalformed(string s) {
		var ex = Assert.Throws<InvalidArgumentException>(() =>
			NativeExports.ParseOptions($$"""{"engineVersion":2,"quirksVersion":"{{s}}"}"""));
		Assert.Equal("quirksVersion", ex.Field);
		Assert.Contains(s, ex.Message);
	}

	[Theory]
	[InlineData("""{"engineVersion":2,"quirksVersion":26}""")]      // number
	[InlineData("""{"engineVersion":2,"quirksVersion":true}""")]    // bool
	[InlineData("""{"engineVersion":2,"quirksVersion":[]}""")]      // array
	[InlineData("""{"engineVersion":2,"quirksVersion":{}}""")]      // object
	public void ParseOptions_QuirksVersion_RejectsNonStringTypes(string json) {
		var ex = Assert.Throws<InvalidArgumentException>(() => NativeExports.ParseOptions(json));
		Assert.Equal("quirksVersion", ex.Field);
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
			CompatCode = DiagnosticCatalog.EventBodyCast.Code,
		};
		var json = NativeExports.SerializeProjectionError(ex);
		using var doc = JsonDocument.Parse(json);
		Assert.Equal(DiagnosticCatalog.EventBodyCast.Code, doc.RootElement.GetProperty("compatCode").GetString());
	}

	[Fact]
	public void SerializeProjectionError_EnrichesCompatCodeFromCatalog() {
		var ex = new InvalidArgumentException("test", "field") {
			CompatCode = DiagnosticCatalog.EventBodyCast.Code,
		};
		var json = NativeExports.SerializeProjectionError(ex);
		using var doc = JsonDocument.Parse(json);
		Assert.Equal(DiagnosticCatalog.EventBodyCast.Message, doc.RootElement.GetProperty("compatDescription").GetString());
		// Every current quirk has FixedIn = null, so compatFixedIn is omitted.
		Assert.False(doc.RootElement.TryGetProperty("compatFixedIn", out _));
	}

	[Fact]
	public void SerializeProjectionError_OmitsEnrichmentForUnknownCompatCode() {
		var ex = new InvalidArgumentException("test", "field") {
			CompatCode = "quirk.not.inCatalog",
		};
		var json = NativeExports.SerializeProjectionError(ex);
		using var doc = JsonDocument.Parse(json);
		Assert.Equal("quirk.not.inCatalog", doc.RootElement.GetProperty("compatCode").GetString());
		Assert.False(doc.RootElement.TryGetProperty("compatDescription", out _));
	}

	[Fact]
	public void SerializeProjectionError_OmitsDiagnosticsWhenEmpty() {
		var ex = new InvalidArgumentException("test", "field");
		var json = NativeExports.SerializeProjectionError(ex);
		using var doc = JsonDocument.Parse(json);
		Assert.False(doc.RootElement.TryGetProperty("diagnostics", out _));
	}

	[Fact]
	public void SerializeProjectionError_EmitsDiagnosticsWhenSet() {
		var ex = new InvalidArgumentException("test", "field") {
			Diagnostics = new[] { DiagnosticCatalog.EventBodyCast.ToDiagnostic() },
		};
		var json = NativeExports.SerializeProjectionError(ex);
		using var doc = JsonDocument.Parse(json);
		var diagnostics = doc.RootElement.GetProperty("diagnostics");
		Assert.Equal(DiagnosticCatalog.EventBodyCast.Code, diagnostics[0].GetProperty("code").GetString());
	}

	// -- SerializeKnownQuirks --

	[Fact]
	public void SerializeKnownQuirks_ReturnsOneEntryPerRegistryItem() {
		var json = NativeExports.SerializeKnownQuirks();
		using var doc = JsonDocument.Parse(json);
		Assert.Equal(JsonValueKind.Array, doc.RootElement.ValueKind);
		Assert.Equal(DiagnosticCatalog.Quirks.Count, doc.RootElement.GetArrayLength());
	}

	[Fact]
	public void SerializeKnownQuirks_EachEntryHasCodeAndDescription() {
		var json = NativeExports.SerializeKnownQuirks();
		using var doc = JsonDocument.Parse(json);
		foreach (var entry in doc.RootElement.EnumerateArray()) {
			Assert.True(entry.TryGetProperty("code", out var code));
			Assert.False(string.IsNullOrEmpty(code.GetString()));
			Assert.True(entry.TryGetProperty("description", out var desc));
			Assert.False(string.IsNullOrEmpty(desc.GetString()));
		}
	}

	[Fact]
	public void SerializeKnownQuirks_OmitsFixedInWhenNull() {
		// Today every entry has FixedIn = null (no upstream fix shipped).
		// All entries should omit the field.
		var json = NativeExports.SerializeKnownQuirks();
		using var doc = JsonDocument.Parse(json);
		foreach (var entry in doc.RootElement.EnumerateArray()) {
			Assert.False(entry.TryGetProperty("fixedIn", out _));
		}
	}

	[Fact]
	public void SerializeKnownQuirks_IncludesAllRegistryCodes() {
		var json = NativeExports.SerializeKnownQuirks();
		using var doc = JsonDocument.Parse(json);
		var codes = doc.RootElement.EnumerateArray()
			.Select(e => e.GetProperty("code").GetString())
			.ToHashSet();
		foreach (var quirk in DiagnosticCatalog.Quirks) {
			Assert.Contains(quirk.Code, codes);
		}
	}
}
