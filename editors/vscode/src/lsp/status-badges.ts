import * as vscode from "vscode";

// A projection's deploy health in one environment, as the LSP server reports it
// per env in the per-projection status lens's `data.healths`. green/orange/red
// are known states; the rest say why there's no reading: locked (needs
// sign-in), error (fetch failed), loading (not yet fetched).
export type BadgeHealth =
	| "green"
	| "orange"
	| "red"
	| "locked"
	| "error"
	| "loading";

export interface BadgeCell {
	range: vscode.Range;
	// Per-environment health in the file's env order.
	healths: BadgeHealth[];
}

// Fill per known health. Fixed hex (not a ThemeColor) since the badge is a
// rendered SVG image; filled circles read on both light and dark themes.
const FILL: Record<"green" | "orange" | "red", string> = {
	green: "#3fb950",
	orange: "#d29922",
	red: "#f85149",
};
// Neutral grey for the no-reading states, distinguished by shape not color.
const NEUTRAL = "#8b949e";
const STROKE = 1.25;

// SVG cell geometry, in viewBox units. Each env gets a CELL-wide slot with a
// circle centered in it; the image renders near line height, so the small
// radius keeps the dots unobtrusive.
const CELL = 13;
const HEIGHT = 12;
const RADIUS = 3.25;

// dotSvg draws one env's badge at (cx, cy): a filled circle for a known health;
// a hollow ring for locked; a hollow ring with a slash for error; a faint
// filled dot for loading (transient, so low emphasis).
function dotSvg(health: BadgeHealth, cx: number, cy: number): string {
	switch (health) {
		case "green":
		case "orange":
		case "red":
			return `<circle cx="${cx}" cy="${cy}" r="${RADIUS}" fill="${FILL[health]}"/>`;
		case "loading":
			return `<circle cx="${cx}" cy="${cy}" r="${RADIUS}" fill="${NEUTRAL}" opacity="0.4"/>`;
		case "locked":
			return `<circle cx="${cx}" cy="${cy}" r="${RADIUS}" fill="none" stroke="${NEUTRAL}" stroke-width="${STROKE}"/>`;
		case "error": {
			const d = RADIUS;
			return (
				`<circle cx="${cx}" cy="${cy}" r="${RADIUS}" fill="none" stroke="${NEUTRAL}" stroke-width="${STROKE}"/>` +
				`<line x1="${cx - d}" y1="${cy + d}" x2="${cx + d}" y2="${cy - d}" stroke="${NEUTRAL}" stroke-width="${STROKE}"/>`
			);
		}
	}
}

// buildHealthRowSvg draws one badge per env health, left to right. Exported for
// direct testing; the runtime wraps it in a data URI.
export function buildHealthRowSvg(healths: readonly BadgeHealth[]): string {
	const width = CELL * Math.max(healths.length, 1);
	const cy = HEIGHT / 2;
	const dots = healths
		.map((health, i) => dotSvg(health, i * CELL + CELL / 2, cy))
		.join("");
	return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${width} ${HEIGHT}" width="${width}" height="${HEIGHT}">${dots}</svg>`;
}

function healthRowUri(healths: readonly BadgeHealth[]): vscode.Uri {
	// base64 the SVG so the data URI body carries no characters Uri parsing
	// would mangle. The SVG is ASCII, so the encoding is lossless.
	const base64 = Buffer.from(buildHealthRowSvg(healths), "utf8").toString(
		"base64",
	);
	return vscode.Uri.parse(`data:image/svg+xml;base64,${base64}`);
}

/**
 * Marks each `[[projection]]` header with a row of inline per-environment health
 * dots, driven by the status data the LSP delivers over the codeLens channel.
 * The row is a generated SVG rendered after the header text (not the gutter),
 * so hovering it reaches the header's status hover - VS Code can't hover a
 * gutter icon.
 *
 * The data arrives per-document (a codeLens response) but decorations are
 * per-editor, so cells are cached by document URI and re-applied whenever an
 * editor showing that document becomes visible - a split or tab switch keeps
 * the markers without waiting for the next status refresh.
 */
export class StatusBadges implements vscode.Disposable {
	readonly #type: vscode.TextEditorDecorationType;
	readonly #byUri = new Map<string, BadgeCell[]>();
	readonly #sub: vscode.Disposable;

	constructor() {
		// One type; the badge row is per-instance content (it varies per
		// projection), set through DecorationOptions.renderOptions.
		this.#type = vscode.window.createTextEditorDecorationType({});
		this.#sub = vscode.window.onDidChangeVisibleTextEditors((editors) => {
			for (const editor of editors) this.#applyTo(editor);
		});
	}

	// set records a document's cells and paints any visible editor showing it.
	// Empty cells clear the document's markers (setDecorations replaces the full
	// set, so a projection that dropped out of the status is cleared).
	set(uri: vscode.Uri, cells: readonly BadgeCell[]): void {
		const key = uri.toString();
		this.#byUri.set(key, [...cells]);
		for (const editor of vscode.window.visibleTextEditors) {
			if (editor.document.uri.toString() === key) this.#applyTo(editor);
		}
	}

	#applyTo(editor: vscode.TextEditor): void {
		const cells = this.#byUri.get(editor.document.uri.toString());
		if (!cells) return;
		const decorations: vscode.DecorationOptions[] = cells.map((c) => ({
			range: c.range,
			renderOptions: {
				after: {
					contentIconPath: healthRowUri(c.healths),
					// Left gap from the header; small top nudge to sit on the text's
					// vertical centre (icon attachments baseline-align by default).
					margin: "2px 0 0 0.6em",
				},
			},
		}));
		editor.setDecorations(this.#type, decorations);
	}

	dispose(): void {
		this.#sub.dispose();
		this.#type.dispose();
	}
}
