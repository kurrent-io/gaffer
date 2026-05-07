namespace Gaffer.Sdk.Versioning;

/// <summary>
/// Registry of KurrentDB features with known introduction versions. Used to
/// gate session configurations that don't exist in older releases.
/// </summary>
public static class KnownFeatures {
	/// <summary>
	/// V2 projection engine architecture. Introduced in KurrentDB 26.1.0
	/// (PR kurrent-io/KurrentDB#5544, commit f8fff69b3).
	/// </summary>
	public static readonly Feature ProjectionsV2 = new() {
		Code = "feature.engine.v2",
		Description = "V2 projection engine architecture.",
		IntroducedIn = new KurrentDbVersion(26, 1, 0),
	};

	/// <summary>All known features, in registry order.</summary>
	public static readonly IReadOnlyList<Feature> All = new[] {
		ProjectionsV2,
	};
}
