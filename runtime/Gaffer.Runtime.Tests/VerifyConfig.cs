using System.Runtime.CompilerServices;

namespace Gaffer.Runtime.Tests;

public static class VerifyConfig {
	[ModuleInitializer]
	public static void Init() =>
		VerifyXunit.Verifier.UseProjectRelativeDirectory("snapshots");
}
