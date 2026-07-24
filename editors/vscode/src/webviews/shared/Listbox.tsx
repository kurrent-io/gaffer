import { createMemo, createSignal, For, type JSX } from "solid-js";
import styles from "./Listbox.module.css";

export interface ListboxProps<T> {
	items: T[];
	getKey: (item: T) => string | number;
	isSelected: (item: T) => boolean;
	onSelect: (item: T) => void;
	renderItem: (item: T) => JSX.Element;
	ariaLabel: string;
	class?: string;
}

// A single-select listbox with the standard roving-tabindex keyboard model:
// exactly one option is tabbable, Up/Down move focus (roving) without
// selecting, Enter/Space select the focused option. Selection is owned by the
// caller (isSelected/onSelect); focus is tracked internally.
export function Listbox<T>(props: ListboxProps<T>) {
	let container: HTMLDivElement | undefined;
	const [focusedKey, setFocusedKey] = createSignal<string | number | null>(
		null,
	);

	// Memoized so each is one O(N) scan per update, not one per rendered option.
	const selectedKey = createMemo(() => {
		const sel = props.items.find((i) => props.isSelected(i));
		return sel ? props.getKey(sel) : null;
	});
	// The tabbable option: whatever was last arrowed to (if still present), else
	// the selected one, else the first.
	const tabbableKey = createMemo(() => {
		const f = focusedKey();
		if (f !== null && props.items.some((i) => props.getKey(i) === f)) return f;
		const s = selectedKey();
		if (s !== null) return s;
		const first = props.items[0];
		return first ? props.getKey(first) : null;
	});

	function moveFocus(from: number, delta: number) {
		if (!container) return;
		const els = Array.from(
			container.querySelectorAll<HTMLElement>('[role="option"]'),
		);
		const next = els[from + delta];
		if (!next) return;
		if (next.dataset.key !== undefined) setFocusedKey(asKey(next.dataset.key));
		next.focus();
	}

	function onKeyDown(e: KeyboardEvent, item: T, index: number) {
		if (e.key === "Enter" || e.key === " ") {
			e.preventDefault();
			props.onSelect(item);
		} else if (e.key === "ArrowDown") {
			e.preventDefault();
			moveFocus(index, 1);
		} else if (e.key === "ArrowUp") {
			e.preventDefault();
			moveFocus(index, -1);
		}
	}

	return (
		<div
			ref={container}
			role="listbox"
			aria-label={props.ariaLabel}
			class={props.class}
		>
			<For each={props.items}>
				{(item, index) => {
					const selected = () => props.isSelected(item);
					const tabbable = () => tabbableKey() === props.getKey(item);
					return (
						<div
							role="option"
							data-key={props.getKey(item)}
							aria-selected={selected() ? "true" : "false"}
							tabindex={tabbable() ? 0 : -1}
							class={`${styles.option} ${selected() ? styles.selected : ""}`}
							onClick={() => {
								setFocusedKey(props.getKey(item));
								props.onSelect(item);
							}}
							onKeyDown={(e) => onKeyDown(e, item, index())}
						>
							{props.renderItem(item)}
						</div>
					);
				}}
			</For>
		</div>
	);
}

// dataset values are strings; recover the numeric key so comparisons against
// getKey (which may return a number) line up.
function asKey(s: string): string | number {
	const n = Number(s);
	return s !== "" && !Number.isNaN(n) ? n : s;
}
