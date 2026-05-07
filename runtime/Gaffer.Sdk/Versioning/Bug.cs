namespace Gaffer.Sdk.Versioning;

/// <summary>
/// A known KurrentDB bug that gaffer reproduces for fidelity. Each bug has a
/// stable <see cref="Code"/>, a description, and an optional
/// <see cref="FixedIn"/> version.
/// <para>
/// A bug fires when the projection session's DB version is unset
/// (<c>null</c> = unversioned, "all bugs on") or earlier than the fix version.
/// Always-buggy items (no upstream fix in flight) leave <see cref="FixedIn"/>
/// as <c>null</c> so they fire in every configuration.
/// </para>
/// </summary>
public sealed record Bug {
	public required string Code { get; init; }
	public required string Description { get; init; }
	public KurrentDbVersion? FixedIn { get; init; }

	/// <summary>True when this bug should be reproduced for the given session version.</summary>
	public bool FiresAt(KurrentDbVersion? dbVersion) =>
		dbVersion is null || FixedIn is null || dbVersion < FixedIn;
}
