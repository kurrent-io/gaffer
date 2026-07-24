import { For, Show } from "solid-js";
import styles from "./SegmentedCounts.module.css";

export interface Segment {
	text: string;
	// Colour class for the segment; the caller owns the domain->colour mapping.
	class?: string | undefined;
}

// A run of emphasised segments joined by " · " separators - a count roll-up
// like "3 to create · 1 to update". Layout only; segments carry their own tone.
export function SegmentedCounts(props: { segments: Segment[] }) {
	return (
		<span>
			<For each={props.segments}>
				{(seg, i) => (
					<>
						<Show when={i() > 0}>
							<span class={styles.sep}> · </span>
						</Show>
						<span class={[styles.seg, seg.class].filter(Boolean).join(" ")}>
							{seg.text}
						</span>
					</>
				)}
			</For>
		</span>
	);
}
