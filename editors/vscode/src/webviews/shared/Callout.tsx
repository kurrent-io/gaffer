import type { JSX } from "solid-js";
import styles from "./Callout.module.css";

// A bordered callout block (left rule + tinted background) for a drift warning
// or a blocking error. Errors announce themselves via role="alert". `class`
// lets the caller add layout (e.g. margin) without the component knowing it.
export function Callout(props: {
	tone?: "warn" | "error";
	class?: string | undefined;
	children: JSX.Element;
}) {
	const tone = () => props.tone ?? "warn";
	return (
		<div
			class={[styles.callout, styles[tone()], props.class]
				.filter(Boolean)
				.join(" ")}
			role={tone() === "error" ? "alert" : undefined}
		>
			{props.children}
		</div>
	);
}
