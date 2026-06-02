using Acornima;
using Acornima.Ast;
using Gaffer.Sdk.Diagnostics;
using Gaffer.Sdk.Versioning;

namespace Gaffer.Runtime.Projection.Diagnostics;

// reorderEvents / processingLag are silently ignored under engine_version 2: V2's
// ReadStrategyFactory never reads them, so events run in arrival order regardless of source.
// Warn so the user finds out the option is dead rather than wondering why ordering didn't
// change. The V1 path is the opposite - KurrentDB's ReaderStrategy rejects reorderEvents on
// the wrong source/lag with a hard error - and is reproduced as a throw at session-create in
// ProjectionSession, off the resolved QuerySources (authoritative; a throw must be exact).
// This rule is the editor-facing V2 hint and reads the AST literally (best-effort, with a
// source range), so it can't see a computed reorderEvents value - acceptable for a warning.
//
// processingLag alone (no reorderEvents) is a no-op on every engine, not a V2 regression, so
// it isn't flagged on its own; it's only surfaced when it rides alongside a reorderEvents
// that V2 drops.
internal sealed class ReorderEventsNoEffectOnV2Rule : IRule {
	public void Run(Script ast, KurrentDbVersion? quirksVersion, ProjectionVersion engineVersion, List<Diagnostic> diagnostics) {
		if (engineVersion != ProjectionVersion.V2)
			return;

		var scanner = new Scanner();
		scanner.Visit(ast);
		// A top-level user `options`/`function options` means the call isn't the builtin,
		// so the analysis can't be trusted - stay quiet. No reorderEvents means nothing to
		// warn about (lone processingLag is a no-op everywhere, not V2-specific).
		if (scanner.OptionsShadowed || scanner.ReorderEventsLocation is not { } reorderLoc)
			return;

		diagnostics.Add(DiagnosticCatalog.ReorderEventsNoEffectOnV2.ToDiagnostic(DiagnosticCollector.ToSourceRange(reorderLoc)));
		if (scanner.ProcessingLagLocation is { } lagLoc)
			diagnostics.Add(DiagnosticCatalog.ReorderEventsNoEffectOnV2.ToDiagnostic(DiagnosticCollector.ToSourceRange(lagLoc)));
	}

	// options is a top-level definition construct, so only calls and shadow declarations at
	// function depth 0 count; an options() in a handler body or nested/dead code is not the
	// source config and must be ignored.
	private sealed class Scanner : AstVisitor {
		private int _functionDepth;

		public bool OptionsShadowed { get; private set; }
		public Acornima.SourceLocation? ReorderEventsLocation { get; private set; }
		public Acornima.SourceLocation? ProcessingLagLocation { get; private set; }

		protected override object? VisitFunctionDeclaration(FunctionDeclaration node) {
			MarkShadow(node.Id);
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

		protected override object? VisitVariableDeclarator(VariableDeclarator node) {
			MarkShadow(node.Id);
			return base.VisitVariableDeclarator(node);
		}

		protected override object? VisitCallExpression(CallExpression node) {
			if (_functionDepth == 0 &&
				node.Callee is Identifier { Name: "options" } &&
				node.Arguments.Count > 0 &&
				node.Arguments[0] is ObjectExpression obj) {
				CollectReorderOptions(obj);
			}
			return base.VisitCallExpression(node);
		}

		private void MarkShadow(Node? id) {
			if (_functionDepth == 0 && id is Identifier { Name: "options" })
				OptionsShadowed = true;
		}

		private void CollectReorderOptions(ObjectExpression obj) {
			foreach (var p in obj.Properties) {
				if (p is not Property { Computed: false } prop)
					continue;
				var key = prop.Key switch {
					Identifier id => id.Name,
					StringLiteral lit => lit.Value,
					_ => null,
				};
				// An explicit `reorderEvents: false` is already off - nothing to warn about. A
				// non-literal value can't be proven off, so warn conservatively. Assigning on
				// every occurrence (clearing on false) keeps last-write-wins across duplicate
				// options() calls, so a later `false` suppresses an earlier `true`.
				if (key == "reorderEvents")
					ReorderEventsLocation = prop.Value is BooleanLiteral { Value: false } ? null : prop.Key.Location;
				else if (key == "processingLag")
					ProcessingLagLocation = prop.Key.Location;
			}
		}
	}
}
