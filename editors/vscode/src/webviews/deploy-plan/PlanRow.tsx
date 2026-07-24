import { For, Show } from "solid-js";
import { Badge } from "../shared/Badge";
import { Button } from "../shared/Button";
import { CodeBlock } from "../shared/CodeBlock";
import { OutcomeLabel } from "./OutcomeLabel";
import { planLabel, rowTags } from "./model";
import type { PlanItem } from "./protocol";
import styles from "./PlanRow.module.css";

// A row's live state during the streaming apply: the settled/in-flight outcome
// that overrides the plan's would-do outcome. undefined while reviewing.
export interface LiveOutcome {
	label: string;
	outcome: string;
	detail: string | undefined;
}

export function PlanRow(props: {
	item: PlanItem;
	live: LiveOutcome | undefined;
	onDiff: (name: string) => void;
}) {
	const outcome = () => props.live?.outcome ?? props.item.outcome;
	const label = () =>
		props.live ? props.live.label : planLabel(props.item.outcome);
	const detail = () =>
		props.live ? props.live.detail : (props.item.error ?? props.item.reason);
	// invalid/failed carry a code diagnostic (compile error, frame); a refusal's
	// reason is prose.
	const isCode = () => outcome() === "invalid" || outcome() === "failed";
	// Diff is offered on the plan's updated rows (there's a deployed version to
	// compare against); it drops away once the apply starts streaming.
	const showDiff = () => !props.live && props.item.outcome === "updated";
	return (
		<li
			class={[styles.row, outcome() === "skipped" && styles.unchanged]
				.filter(Boolean)
				.join(" ")}
		>
			<span class={styles.name}>{props.item.name}</span>
			<OutcomeLabel outcome={outcome()} text={label()} />
			<For each={rowTags(props.item)}>
				{(tag) => <Badge tone="warn">{tag}</Badge>}
			</For>
			<Show when={showDiff()}>
				<Button
					variant="action"
					class={styles.action}
					aria-label={`Diff ${props.item.name}`}
					onClick={() => props.onDiff(props.item.name)}
				>
					Diff
				</Button>
			</Show>
			<Show when={detail()}>
				{(text) => (
					<div class={styles.detailLine}>
						<Show
							when={isCode()}
							fallback={<span class={styles.detailText}>{text()}</span>}
						>
							<CodeBlock>{text()}</CodeBlock>
						</Show>
					</div>
				)}
			</Show>
		</li>
	);
}
