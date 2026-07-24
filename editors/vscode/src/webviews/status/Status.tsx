import { Index, Show } from "solid-js";
import { Button } from "../shared/Button";
import type { StatusUpdateMessage } from "./protocol";
import styles from "./Status.module.css";

export interface StatusProps {
	update: StatusUpdateMessage | null;
	onPause: () => void;
}

// Running/ended status pane: a title (with a warning glyph on failure), an
// optional error body, the live stats, and the pause button. Presentational
// only - all state is computed host-side and arrives via `update`.
export function Status(props: StatusProps) {
	return (
		<div class={styles.root}>
			<Show
				when={props.update}
				fallback={<div class={styles.stat}>Connecting...</div>}
			>
				{(u) => (
					<>
						<div class={styles.titleRow}>
							<Show when={u().error}>
								<span class={styles.titleIcon} aria-hidden="true">
									{"⚠"}
								</span>
							</Show>
							<span class={styles.title}>{u().title}</span>
						</div>
						<Show when={u().error}>
							<div class={styles.error} role="alert">
								{u().error}
							</div>
						</Show>
						<div class={styles.stats}>
							<Index each={u().stats}>
								{(stat) => <div class={styles.stat}>{stat()}</div>}
							</Index>
						</div>
						<Show when={u().showPauseButton}>
							<Button
								variant="primary"
								disabled={u().pauseButtonDisabled}
								onClick={props.onPause}
							>
								{u().pauseButtonLabel}
							</Button>
						</Show>
						<Show when={u().mode === "ended"}>
							<div class={styles.caption}>State preserved until next run.</div>
						</Show>
					</>
				)}
			</Show>
		</div>
	);
}
