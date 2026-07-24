import { Show } from "solid-js";
import { Button } from "../shared/Button";
import { KeyValueGrid, Field } from "../shared/KeyValueGrid";
import { NoteLine } from "../shared/NoteLine";
import { SectionHeading } from "../shared/SectionHeading";
import { StatusDot } from "../shared/StatusDot";
import { ToneText } from "../shared/ToneText";
import {
	detailNote,
	fmtTime,
	hasPreviousContent,
	matchesEarlier,
	rollable,
	runState,
	shortHash,
	shortRev,
	verbLabel,
	verbTone,
} from "./model";
import type { HistoryEntry, HistoryGraph } from "./protocol";
import styles from "./DetailPanel.module.css";

export interface DetailPanelProps {
	entry: HistoryEntry | undefined;
	entries: HistoryEntry[];
	graph: HistoryGraph;
	// The version with a rollback in flight (its actions read "rolling back…"),
	// or null. A live rollback blocks a second one whatever is selected.
	rollingVersion: number | null;
	onRollback: (version: number) => void;
	onDiff: (version: number, framing: "previous" | "local") => void;
}

function hasMeta(e: HistoryEntry): boolean {
	return !!(e.tool || e.operation || e.actor || e.revision);
}

export function DetailPanel(props: DetailPanelProps) {
	return (
		<div class={styles.panel}>
			<div class={styles.panelHead}>
				<Show when={props.entry}>
					{(e) => (
						<>
							<StatusDot state={runState(e())} />
							<ToneText tone={verbTone(e())} class={styles.verb}>
								{verbLabel(e())}
							</ToneText>
						</>
					)}
				</Show>
			</div>
			<div class={styles.panelBody}>
				<Show when={props.entry}>
					{(e) => (
						<>
							<div class={styles.content}>
								<KeyValueGrid>
									<Field label="when" value={fmtTime(e().time)} />
									<Field label="state" value={runState(e())} />
									<Show when={rollable(e())}>
										<Field
											label="content"
											value={shortHash(e().contentHash)}
											valueClass={styles.contentHash}
										/>
										<Show
											when={matchesEarlier(
												props.entries,
												props.graph,
												e().version,
											)}
										>
											<NoteLine class={styles.gridNote} icon="↩">
												matches an earlier deploy
											</NoteLine>
										</Show>
									</Show>
									<Show when={detailNote(e())}>
										{(note) => (
											<NoteLine
												class={styles.gridNote}
												tone={note().warn ? "warn" : "muted"}
												icon={note().warn ? "⚠" : undefined}
											>
												{note().text}
											</NoteLine>
										)}
									</Show>
								</KeyValueGrid>
								<Show when={hasMeta(e())}>
									<SectionHeading>version metadata</SectionHeading>
									<KeyValueGrid>
										<Show when={e().actor}>
											{(actor) => <Field label="actor" value={actor()} />}
										</Show>
										<Show when={e().tool}>
											{(tool) => (
												<Field
													label="tool"
													value={
														tool() +
														(e().toolVersion ? ` ${e().toolVersion}` : "")
													}
												/>
											)}
										</Show>
										<Show when={e().operation}>
											{(op) => <Field label="operation" value={op()} />}
										</Show>
										<Show when={e().revision}>
											{(rev) => (
												<Field label="source" value={shortRev(rev())} />
											)}
										</Show>
									</KeyValueGrid>
								</Show>
							</div>
							<div class={styles.actions}>
								<Show
									when={props.rollingVersion !== null}
									fallback={
										<Show when={rollable(e())}>
											<Button
												variant="secondary"
												onClick={() => props.onRollback(e().version)}
											>
												roll back
											</Button>
											<Button
												variant="secondary"
												disabled={
													!hasPreviousContent(props.entries, e().version)
												}
												title={
													hasPreviousContent(props.entries, e().version)
														? undefined
														: "No earlier version to compare"
												}
												onClick={() => props.onDiff(e().version, "previous")}
											>
												diff previous
											</Button>
											<Button
												variant="secondary"
												onClick={() => props.onDiff(e().version, "local")}
											>
												diff local
											</Button>
										</Show>
									}
								>
									<span class={styles.rolling} role="status">
										{e().version === props.rollingVersion
											? "rolling back…"
											: "rolling back another version…"}
									</span>
								</Show>
							</div>
						</>
					)}
				</Show>
			</div>
		</div>
	);
}
