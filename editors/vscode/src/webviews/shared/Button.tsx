import { splitProps, type JSX } from "solid-js";
import styles from "./Button.module.css";

export type ButtonVariant = "primary" | "secondary" | "action";

export interface ButtonProps extends JSX.ButtonHTMLAttributes<HTMLButtonElement> {
	variant?: ButtonVariant;
}

// Themed button; primary and secondary map to the editor's button colours,
// action is a small inline secondary (row actions). All native button attrs
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
