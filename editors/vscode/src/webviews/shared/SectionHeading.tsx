import type { JSX } from "solid-js";
import styles from "./SectionHeading.module.css";

// A small uppercase, muted label separating sections of a detail view.
export function SectionHeading(props: { children: JSX.Element }) {
	return <div class={styles.section}>{props.children}</div>;
}
