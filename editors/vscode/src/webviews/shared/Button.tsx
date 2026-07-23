import { splitProps, type JSX } from "solid-js";
import styles from "./Button.module.css";

export type ButtonVariant = "primary" | "secondary" | "action";

export interface ButtonProps extends JSX.ButtonHTMLAttributes<HTMLButtonElement> {
	variant?: ButtonVariant;
}

// Themed button. `action` is the small inline variant (row actions); primary
// and secondary map to the editor's button colours. All native button attrs
// (onClick, disabled, title, ...) pass through.
export function Button(props: ButtonProps) {
	const [local, rest] = splitProps(props, ["variant", "class", "children"]);
	const cls = () =>
		[styles.btn, styles[local.variant ?? "secondary"], local.class]
			.filter(Boolean)
			.join(" ");
	return (
		<button {...rest} class={cls()}>
			{local.children}
		</button>
	);
}
