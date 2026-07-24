import type { JSX } from "solid-js";
import styles from "./Badge.module.css";

// A small pill tag. `neutral` uses the editor's badge colours; `warn` the
// input-validation warning colours.
export function Badge(props: {
	tone?: "neutral" | "warn";
	children: JSX.Element;
}) {
	return (
		<span class={`${styles.badge} ${styles[props.tone ?? "neutral"]}`}>
			{props.children}
		</span>
	);
}
