using System.Globalization;

namespace Gaffer.Sdk.Versioning;

/// <summary>
/// A KurrentDB release version. Three-component <c>MAJOR.MINOR.PATCH</c>.
/// KurrentDB doesn't strictly use semver, but the three-component shape is
/// consistent across releases.
/// </summary>
public sealed record KurrentDbVersion(int Major, int Minor, int Patch)
	: IComparable<KurrentDbVersion> {

	public static KurrentDbVersion Parse(string s) {
		if (TryParse(s, out var version))
			return version;
		throw new FormatException(
			$"'{s}' is not a valid KurrentDB version. Expected MAJOR.MINOR.PATCH (e.g. 26.1.0).");
	}

	public static bool TryParse(string? s, out KurrentDbVersion version) {
		version = new KurrentDbVersion(0, 0, 0);
		if (string.IsNullOrWhiteSpace(s))
			return false;
		var parts = s.Split('.');
		if (parts.Length != 3)
			return false;
		if (!int.TryParse(parts[0], NumberStyles.None, CultureInfo.InvariantCulture, out var major) ||
			!int.TryParse(parts[1], NumberStyles.None, CultureInfo.InvariantCulture, out var minor) ||
			!int.TryParse(parts[2], NumberStyles.None, CultureInfo.InvariantCulture, out var patch))
			return false;
		version = new KurrentDbVersion(major, minor, patch);
		return true;
	}

	public int CompareTo(KurrentDbVersion? other) {
		if (other is null)
			return 1;
		var c = Major.CompareTo(other.Major);
		if (c != 0)
			return c;
		c = Minor.CompareTo(other.Minor);
		if (c != 0)
			return c;
		return Patch.CompareTo(other.Patch);
	}

	public static bool operator <(KurrentDbVersion a, KurrentDbVersion b) => a.CompareTo(b) < 0;
	public static bool operator >(KurrentDbVersion a, KurrentDbVersion b) => a.CompareTo(b) > 0;
	public static bool operator <=(KurrentDbVersion a, KurrentDbVersion b) => a.CompareTo(b) <= 0;
	public static bool operator >=(KurrentDbVersion a, KurrentDbVersion b) => a.CompareTo(b) >= 0;

	public override string ToString() => $"{Major}.{Minor}.{Patch}";
}
