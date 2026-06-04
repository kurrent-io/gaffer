using System.Text.Json;
using Gaffer.Runtime.Errors;
using Gaffer.Sdk.Diagnostics;
using Gaffer.Sdk.Versioning;

namespace Gaffer.Runtime.Tests;

/// <summary>
/// Pins the JSON wire format that bindings + CLI rely on:
/// - options going in (quirksVersion accepted, parsed, validated)
/// - errors coming out (compatCode, and the catalogue-enriched compatDescription/compatFixedIn)
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
		// EventBodyCast is fixed upstream in 26.2.0 (PR #5610), so the enrichment surfaces compatFixedIn.
		Assert.Equal("26.2.0", doc.RootElement.GetProperty("compatFixedIn").GetString());
	}

	[Fact]
	public void SerializeProjectionError_OmitsCompatFixedIn_WhenQuirkHasNoFix() {
		// LogMultiParam has no upstream fix (FixedIn = null), so compatFixedIn is omitted.
		var ex = new InvalidArgumentException("test", "field") {
			CompatCode = DiagnosticCatalog.LogMultiParam.Code,
		};
		var json = NativeExports.SerializeProjectionError(ex);
		using var doc = JsonDocument.Parse(json);
		Assert.Equal(DiagnosticCatalog.LogMultiParam.Message, doc.RootElement.GetProperty("compatDescription").GetString());
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
}
