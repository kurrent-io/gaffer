namespace Gaffer.Sdk.Versioning;

/// <summary>
/// A KurrentDB feature with a known introduction version. Used to gate
/// configurations that don't make sense in earlier versions (e.g. V2 engine
/// against a DB that doesn't have V2).
/// </summary>
public sealed record Feature {
	public required string Code { get; init; }
	public required string Description { get; init; }
	public required KurrentDbVersion IntroducedIn { get; init; }

	/// <summary>
	/// True when the feature is available in the given session version. An
	/// unset version (<c>null</c>) is treated as "permissive" - all features
	/// are available, matching the unversioned-defaults model.
	/// </summary>
	public bool AvailableAt(KurrentDbVersion? quirksVersion) =>
		quirksVersion is null || quirksVersion >= IntroducedIn;
}
