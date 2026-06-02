using Acornima;
using Acornima.Ast;
using Gaffer.Sdk.Diagnostics;
using Gaffer.Sdk.Versioning;

namespace Gaffer.Runtime.Projection.Diagnostics;

// async / Promise-returning handlers silently produce empty state. The
// projection engine (Jint) is synchronous with no event loop, so a handler
// that returns a Promise has it serialized as the state - and a Promise has
// no enumerable own properties, so the state becomes {} - rather than being
// awaited. This matches KurrentDB but surprises users authoring tests in an
// async-capable JS runtime, so warn at compile time.
//
// Scoped to the handler functions passed to when({...}) / chainHandlers({...}),
// and only to a handler's *own* return value: an async helper or a Promise
// returned from a function nested inside a handler doesn't determine the
// handler's return, so flagging it would be misleading. The Promise check is a
// literal-syntax heuristic (new Promise(...) / Promise.x(...)) and doesn't
// account for a shadowed Promise global. Not quirk- or version-gated.
internal sealed class AsyncHandlerRule : IRule {
	public void Run(Script ast, KurrentDbVersion? quirksVersion, ProjectionVersion engineVersion, List<Diagnostic> diagnostics) {
		var scanner = new HandlerScanner();
		scanner.Visit(ast);

		foreach (var loc in scanner.AsyncHandlers) {
			diagnostics.Add(DiagnosticCatalog.HandlerAsync.ToDiagnostic(DiagnosticCollector.ToSourceRange(loc)));
		}
		foreach (var loc in scanner.PromiseReturns) {
			diagnostics.Add(DiagnosticCatalog.HandlerPromise.ToDiagnostic(DiagnosticCollector.ToSourceRange(loc)));
		}
	}

	private static bool IsPromiseConstruction(Node? expr) => expr switch {
		NewExpression { Callee: Identifier { Name: "Promise" } } => true,
		CallExpression { Callee: MemberExpression { Computed: false, Object: Identifier { Name: "Promise" } } } => true,
		_ => false,
	};

	// Finds when({...}) / chainHandlers({...}) calls and inspects each
	// handler-function value for an async modifier or a direct Promise return.
	private sealed class HandlerScanner : AstVisitor {
		public List<Acornima.SourceLocation> AsyncHandlers { get; } = new();
		public List<Acornima.SourceLocation> PromiseReturns { get; } = new();

		protected override object? VisitCallExpression(CallExpression node) {
			if (node.Callee is MemberExpression { Computed: false, Property: Identifier { Name: "when" or "chainHandlers" } } &&
				node.Arguments.Count > 0 &&
				node.Arguments[0] is ObjectExpression obj) {
				foreach (var p in obj.Properties) {
					if (p is Property { Computed: false } prop)
						AnalyzeHandler(prop.Value);
				}
			}
			return base.VisitCallExpression(node);
		}

		private void AnalyzeHandler(Node handler) {
			switch (handler) {
				case FunctionExpression fn:
					if (fn.Async)
						AsyncHandlers.Add(fn.Location);
					CollectDirectPromiseReturns(fn.Body);
					break;
				case ArrowFunctionExpression arrow:
					if (arrow.Async)
						AsyncHandlers.Add(arrow.Location);
					// Concise-body arrow `Ping: (s, e) => Promise.resolve(...)`
					// returns the body expression directly.
					if (arrow.Body is Expression body) {
						if (IsPromiseConstruction(body))
							PromiseReturns.Add(body.Location);
					} else {
						CollectDirectPromiseReturns(arrow.Body);
					}
					break;
			}
		}

		private void CollectDirectPromiseReturns(Node body) {
			var returns = new DirectReturnScanner();
			returns.Visit(body);
			PromiseReturns.AddRange(returns.PromiseReturns);
		}
	}

	// Collects Promise-returning return statements at the handler body's own
	// depth, descending into but ignoring returns inside nested functions
	// (those are some other function's return, not the handler's).
	private sealed class DirectReturnScanner : AstVisitor {
		private int _functionDepth;

		public List<Acornima.SourceLocation> PromiseReturns { get; } = new();

		protected override object? VisitFunctionDeclaration(FunctionDeclaration node) {
			_functionDepth++;
			var result = base.VisitFunctionDeclaration(node);
			_functionDepth--;
			return result;
		}

		protected override object? VisitFunctionExpression(FunctionExpression node) {
			_functionDepth++;
			var result = base.VisitFunctionExpression(node);
			_functionDepth--;
			return result;
		}

		protected override object? VisitArrowFunctionExpression(ArrowFunctionExpression node) {
			_functionDepth++;
			var result = base.VisitArrowFunctionExpression(node);
			_functionDepth--;
			return result;
		}

		protected override object? VisitReturnStatement(ReturnStatement node) {
			if (_functionDepth == 0 && node.Argument is { } arg && IsPromiseConstruction(arg))
				PromiseReturns.Add(arg.Location);
			return base.VisitReturnStatement(node);
		}
	}
}
