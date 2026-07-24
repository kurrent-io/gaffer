import type { JSX } from "solid-js";
import styles from "./Checkbox.module.css";

// A labelled checkbox. The whole label toggles it; state is caller-owned.
export function Checkbox(props: {
	checked: boolean;
	onChange: (checked: boolean) => void;
	children: JSX.Element;
}) {
	return (
		<label class={styles.label}>
			<input
				type="checkbox"
				checked={props.checked}
				onChange={(e) => props.onChange(e.currentTarget.checked)}
			/>
			{props.children}
		</label>
	);
}
