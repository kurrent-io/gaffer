import type { JSX } from "solid-js";
import styles from "./Notice.module.css";

// A standalone message line - an empty-state ("info", italic muted) or a load
// failure ("error"). Errors announce themselves via role="alert".
export function Notice(props: {
	tone?: "info" | "error";
	children: JSX.Element;
}) {
	const error = () => props.tone === "error";
	return (
		<div
			class={`${styles.notice} ${error() ? styles.error : ""}`}
			role={error() ? "alert" : undefined}
		>
			{props.children}
		</div>
	);
}
