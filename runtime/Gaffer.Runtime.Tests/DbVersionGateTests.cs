using Gaffer.Runtime.Errors;
using Gaffer.Sdk.Versioning;

namespace Gaffer.Runtime.Tests;

public class DbVersionGateTests {
	private const string TrivialProjection = "fromAll().when({ $any: function (s, e) { return s; } });";

	[Fact]
	public void V2WithUnversioned_Accepted() {
		// Unversioned is permissive on features - V2 always available without an explicit version.
		using var session = new ProjectionSession(
			TrivialProjection,
			new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });
	}

	[Fact]
	public void V2With26_1_0_Accepted() {
		using var session = new ProjectionSession(
			TrivialProjection,
			new ProjectionSessionOptions {
				EngineVersion = ProjectionVersion.V2,
				DbVersion = new KurrentDbVersion(26, 1, 0),
			});
	}

	[Fact]
	public void V2With26_1_1_Accepted() {
		using var session = new ProjectionSession(
			TrivialProjection,
			new ProjectionSessionOptions {
				EngineVersion = ProjectionVersion.V2,
				DbVersion = new KurrentDbVersion(26, 1, 1),
			});
	}

	[Fact]
	public void V2With26_0_0_Rejected() {
		// V2 didn't ship until 26.1.0; targeting an older version should fail
		// at construction with a descriptive error, not propagate further.
		var ex = Assert.Throws<InvalidArgumentException>(() =>
			new ProjectionSession(
				TrivialProjection,
				new ProjectionSessionOptions {
					EngineVersion = ProjectionVersion.V2,
					DbVersion = new KurrentDbVersion(26, 0, 0),
				}));

		Assert.Contains("V2", ex.Message);
		Assert.Contains("26.1.0", ex.Message);
		// Field name matches the JSON option key, matching ParseDbVersion's
		// other validation throws and the bindings' field-name contract.
		Assert.Equal("dbVersion", ex.Field);
	}

	[Fact]
	public void V1WithAnyVersion_Accepted() {
		using var s1 = new ProjectionSession(
			TrivialProjection,
			new ProjectionSessionOptions {
				EngineVersion = ProjectionVersion.V1,
				DbVersion = new KurrentDbVersion(25, 0, 0),
			});

		using var s2 = new ProjectionSession(
			TrivialProjection,
			new ProjectionSessionOptions {
				EngineVersion = ProjectionVersion.V1,
				DbVersion = new KurrentDbVersion(27, 0, 0),
			});
	}

	[Fact]
	public void V1WithUnversioned_Accepted() {
		using var session = new ProjectionSession(
			TrivialProjection,
			new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V1 });
	}
}
