import type { JSX } from "solid-js";
import styles from "./CodeBlock.module.css";

// A monospace block for code/diagnostics. Whitespace is preserved but
// overlong lines wrap in place: a compile frame keeps its columns until a
// line genuinely exceeds the panel, where wrapping beats a sideways scroll
// that reads as clipped.
export function CodeBlock(props: { children: JSX.Element }) {
	return <div class={styles.code}>{props.children}</div>;
}
