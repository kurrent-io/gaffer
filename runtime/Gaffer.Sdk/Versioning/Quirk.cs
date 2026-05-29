namespace Gaffer.Sdk.Versioning;

/// <summary>
/// A known KurrentDB quirk that gaffer reproduces for fidelity. Each quirk has a
/// stable <see cref="Code"/>, a description, and an optional
/// <see cref="FixedIn"/> version.
/// <para>
/// A quirk fires when the projection session's DB version is unset
/// (<c>null</c> = unversioned, "all quirks on") or earlier than the fix version.
/// Always-present quirks (no upstream fix in flight) leave <see cref="FixedIn"/>
/// as <c>null</c> so they fire in every configuration.
/// </para>
/// </summary>
public sealed record Quirk {
	public required string Code { get; init; }
	public required string Description { get; init; }
	public KurrentDbVersion? FixedIn { get; init; }

	/// <summary>True when this quirk should be reproduced for the given session version.</summary>
	public bool FiresAt(KurrentDbVersion? quirksVersion) =>
		quirksVersion is null || FixedIn is null || quirksVersion < FixedIn;
}
