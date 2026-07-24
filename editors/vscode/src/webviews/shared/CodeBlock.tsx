import type { JSX } from "solid-js";
import styles from "./CodeBlock.module.css";

// A monospace block for code/diagnostics that scrolls horizontally rather than
// wrapping (so a compile frame keeps its columns).
export function CodeBlock(props: { children: JSX.Element }) {
	return <div class={styles.code}>{props.children}</div>;
}
