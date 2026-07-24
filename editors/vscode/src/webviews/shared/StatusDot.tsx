import styles from "./StatusDot.module.css";

export type RunState = "enabled" | "disabled" | "deleted";

// A small filled dot coloured by run state. Decorative (aria-hidden) - the run
// state is always also carried in text next to it.
export function StatusDot(props: { state: RunState }) {
	return (
		<span class={`${styles.dot} ${styles[props.state]}`} aria-hidden="true" />
	);
}
