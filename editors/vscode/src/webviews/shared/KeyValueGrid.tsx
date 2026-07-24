import type { JSX } from "solid-js";
import styles from "./KeyValueGrid.module.css";

// A two-column key/value grid for detail views. Children are <Field> rows;
// a caller may also drop a value-column note in via its own grid-column: 2 class.
export function KeyValueGrid(props: { children: JSX.Element }) {
	return <div class={styles.grid}>{props.children}</div>;
}

// One key/value row: two grid cells. `valueClass` styles the value cell for
// domain-specific values (e.g. a content-hash tint) without the shared
// component knowing the domain.
export function Field(props: {
	label: string;
	value: JSX.Element;
	valueClass?: string | undefined;
}) {
	return (
		<>
			<span class={styles.key}>{props.label}</span>
			<span class={[styles.val, props.valueClass].filter(Boolean).join(" ")}>
				{props.value}
			</span>
		</>
	);
}
