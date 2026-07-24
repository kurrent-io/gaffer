import { Show, type JSX } from "solid-js";
import styles from "./NoteLine.module.css";

export interface NoteLineProps {
	tone?: "muted" | "warn";
	// Decorative leading glyph (e.g. ⚠ or ↩); announced only through the text.
	icon?: string | undefined;
	// CSS-module class names are typed string | undefined here (the tsconfig's
	// noUncheckedIndexedAccess), so accept that rather than force casts at every
	// call site.
	class?: string | undefined;
	children: JSX.Element;
}

// A dimmed secondary line - provenance, a caution, "matches an earlier deploy".
// Layout (margins, grid placement) is left to the caller via `class`.
export function NoteLine(props: NoteLineProps) {
	const cls = () =>
		[styles.note, props.tone === "warn" && styles.warn, props.class]
			.filter(Boolean)
			.join(" ");
	return (
		<div class={cls()}>
			<Show when={props.icon}>
				<span aria-hidden="true">{props.icon} </span>
			</Show>
			{props.children}
		</div>
	);
}
