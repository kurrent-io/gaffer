namespace Gaffer.Sdk.Diagnostics;

/// <summary>
/// The class of a <see cref="Diagnostic"/> - the top segment of its
/// <see cref="Diagnostic.Code"/> (<c>&lt;class&gt;.&lt;subject&gt;.&lt;detail&gt;</c>).
/// </summary>
public enum DiagnosticClass {
	/// <summary>
	/// A KurrentDB engine bug gaffer reproduces for fidelity. Gated by the session's quirks
	/// version (see <see cref="DiagnosticDescriptor.FiresAt"/>); opt out by setting it. Codes
	/// are prefixed <c>quirk.</c>.
	/// </summary>
	Quirk,

	/// <summary>
	/// Something about the user's own projection code - a misuse, an unsupported construct, a
	/// deprecation, or an engine-version behavioural note. Codes are prefixed <c>usage.</c>.
	/// </summary>
	Usage,
}
