using Acornima;
using Acornima.Ast;

namespace Gaffer.Runtime.Projection.Diagnostics;

/// <summary>
/// Scans for calls to a named global identifier, with shadow detection.
/// A top-level <c>var</c>/<c>function</c> declaration of the same name
/// flips <see cref="Shadowed"/>; rules that depend on the global then
/// suppress their diagnostics. <paramref name="matchCall"/> filters which
/// calls land in <see cref="Calls"/> (e.g. arity gate).
/// </summary>
internal sealed class IdentifierShadowScanner : AstVisitor {
	private readonly string _name;
	private readonly Func<CallExpression, bool> _matchCall;

	public IdentifierShadowScanner(string name, Func<CallExpression, bool> matchCall) {
		_name = name;
		_matchCall = matchCall;
	}

	public bool Shadowed { get; private set; }
	public List<Acornima.SourceLocation> Calls { get; } = new();

	protected override object? VisitVariableDeclarator(VariableDeclarator node) {
		if (node.Id is Identifier id && id.Name == _name)
			Shadowed = true;
		return base.VisitVariableDeclarator(node);
	}

	protected override object? VisitFunctionDeclaration(FunctionDeclaration node) {
		if (node.Id is Identifier id && id.Name == _name)
			Shadowed = true;
		return base.VisitFunctionDeclaration(node);
	}

	protected override object? VisitCallExpression(CallExpression node) {
		if (node.Callee is Identifier callee && callee.Name == _name && _matchCall(node))
			Calls.Add(callee.Location);
		return base.VisitCallExpression(node);
	}
}

/// <summary>
/// Scans for chained method calls of a named property (e.g. <c>x.foo()</c>).
/// Used for transforms/outputState which are chain methods on the
/// projection runtime, not globals - so shadow detection (which exists
/// for global identifiers in <see cref="IdentifierShadowScanner"/>)
/// doesn't apply: a property name on a chain object can't be shadowed
/// by a top-level <c>var</c>/<c>function</c>.
/// </summary>
internal sealed class MemberCallScanner : AstVisitor {
	private readonly string _name;

	public MemberCallScanner(string name) {
		_name = name;
	}

	public List<Acornima.SourceLocation> Calls { get; } = new();

	protected override object? VisitCallExpression(CallExpression node) {
		if (node.Callee is MemberExpression me &&
			!me.Computed &&
			me.Property is Identifier { Name: var propName } prop &&
			propName == _name) {
			Calls.Add(prop.Location);
		}
		return base.VisitCallExpression(node);
	}
}
