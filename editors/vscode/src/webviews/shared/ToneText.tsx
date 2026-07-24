import { splitProps, type JSX } from "solid-js";
import type { Tone } from "./tone";
import styles from "./ToneText.module.css";

export interface ToneTextProps extends JSX.HTMLAttributes<HTMLSpanElement> {
	tone: Tone;
}

// A span coloured by a semantic tone. The mapping from a domain value to a tone
// lives with the caller; this only paints.
export function ToneText(props: ToneTextProps) {
	const [local, rest] = splitProps(props, ["tone", "class", "children"]);
	const cls = () => [styles[local.tone], local.class].filter(Boolean).join(" ");
	return (
		<span {...rest} class={cls()}>
			{local.children}
		</span>
	);
}
