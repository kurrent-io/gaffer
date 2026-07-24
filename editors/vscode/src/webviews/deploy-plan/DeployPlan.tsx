import {
	createEffect,
	createSignal,
	For,
	on,
	Show,
	Switch,
	Match,
} from "solid-js";
import { Button } from "../shared/Button";
import { Callout } from "../shared/Callout";
import { Checkbox } from "../shared/Checkbox";
import { SegmentedCounts, type Segment } from "../shared/SegmentedCounts";
import { doneHeadline, planSummarySegments, resultSegments } from "./model";
import outcomes from "./outcomes.module.css";
import { PlanRow, type LiveOutcome } from "./PlanRow";
import type {
	DeployInbound,
	DeployOutbound,
	DeploySummaryCounts,
	PlanReport,
} from "./protocol";
import styles from "./DeployPlan.module.css";

const PROD_BLOCKED =
	"This is a production database, so invalid projections can't be skipped. Fix them to deploy.";

export function DeployPlan(props: {
	inbox: DeployInbound[];
	onDrained: () => void;
	post: (message: DeployOutbound) => void;
}) {
	const [report, setReport] = createSignal<PlanReport | null>(null);
	const [token, setToken] = createSignal(0);
	const [phase, setPhase] = createSignal<
		"reviewing" | "applying" | "done" | "error"
	>("reviewing");
	const [live, setLive] = createSignal<Record<string, LiveOutcome>>({});
	const [summary, setSummary] = createSignal<DeploySummaryCounts | null>(null);
	const [errorMsg, setErrorMsg] = createSignal("");
	const [bypass, setBypass] = createSignal(false);
	// Disables Deploy the moment it's clicked, covering the gap until the host's
	// deploy-started switches the footer into progress mode.
	const [submitted, setSubmitted] = createSignal(false);

	// Drain the inbox in order, applying every queued message, then clear it - so
	// no streaming delta is dropped even if several land before this runs. on()
	// tracks only props.inbox (not the signals handle() writes); clearing can't
	// lose a message because handle() is synchronous.
	createEffect(
		on(
			() => props.inbox,
			(inbox) => {
				if (inbox.length === 0) return;
				for (const msg of inbox) handle(msg);
				props.onDrained();
			},
		),
	);

	function handle(msg: DeployInbound) {
		switch (msg.type) {
			case "plan":
				setReport(msg.report);
				setToken(msg.token);
				setPhase("reviewing");
				setLive({});
				setSummary(null);
				setErrorMsg("");
				setBypass(false);
				setSubmitted(false);
				break;
			case "deploy-started":
				setPhase("applying");
				break;
			case "deploy-active":
				setLive((l) => ({
					...l,
					[msg.name]: {
						label: "applying…",
						outcome: "applying",
						detail: undefined,
					},
				}));
				break;
			case "deploy-item":
				setLive((l) => ({
					...l,
					[msg.name]: {
						label: msg.outcome,
						outcome: msg.outcome,
						detail: msg.detail,
					},
				}));
				break;
			case "deploy-done":
				setSummary(msg.summary);
				setPhase("done");
				break;
			case "deploy-error":
				setErrorMsg(msg.message);
				setPhase("error");
				// Any row left spinning at "applying…" never settled - clear it to a
				// dash rather than imply work is still running.
				setLive((l) => {
					const next = { ...l };
					for (const name of Object.keys(next)) {
						if (next[name]?.outcome === "applying") {
							next[name] = {
								label: "—",
								outcome: "skipped",
								detail: undefined,
							};
						}
					}
					return next;
				});
				break;
		}
	}

	const items = () => report()?.plan ?? [];
	const nameCol = () =>
		`${items().reduce((m, i) => Math.max(m, i.name.length), 0) + 2}ch`;
	const drift = () => report()?.configDrift ?? [];

	const summarySegs = (): Segment[] =>
		planSummarySegments(items()).map((s) => ({
			text: s.text,
			class: outcomes[s.outcome],
		}));

	const doneSegs = (): Segment[] => {
		const s = summary();
		if (!s) return [];
		const head = doneHeadline(s);
		return [
			{ text: head.text, class: head.ok ? styles.ok : styles.fail },
			...resultSegments(s).map((seg) => ({
				text: seg.text,
				class: outcomes[seg.outcome],
			})),
		];
	};

	const cancel = () => props.post({ command: "cancel" });
	const deploy = (noValidate: boolean) => {
		setSubmitted(true);
		props.post({ command: "deploy", noValidate, token: token() });
	};

	// When the footer swaps to a progress/result message, the focused Deploy
	// button unmounts; move focus to the message so a keyboard/AT user isn't
	// dropped to the document body, and the live region reads it.
	const focusOnMount = (el: HTMLElement) => {
		el.tabIndex = -1;
		queueMicrotask(() => el.focus());
	};

	return (
		<div class={styles.root}>
			<Show when={report()}>
				{(r) => (
					<>
						<div class={styles.header}>
							<div class={styles.headerLeft}>
								<span class={styles.env}>
									{r().target || r().env || "environment"}
								</span>
								<Show when={r().production}>
									<span class={styles.prod}>production</span>
								</Show>
							</div>
							<Show when={r().env}>
								<span class={styles.envKey}>env.{r().env}</span>
							</Show>
						</div>

						<Show
							when={r().configDriftError}
							fallback={
								<Show when={drift().length > 0}>
									<Callout tone="warn" class={styles.drift}>
										{`[database_config] diverges from the target:\n${drift()
											.map(
												(d) =>
													`${d.knob}: server ${d.server}, local ${d.local}`,
											)
											.join("\n")}`}
									</Callout>
								</Show>
							}
						>
							{(err) => (
								<Callout tone="warn" class={styles.drift}>
									{`[database_config] check couldn't run: ${err()}`}
								</Callout>
							)}
						</Show>

						<ul class={styles.plan} style={{ "--name-col": nameCol() }}>
							<Show
								when={items().length > 0}
								fallback={
									<li>
										<span class={styles.empty}>No projections configured.</span>
									</li>
								}
							>
								<For each={items()}>
									{(item) => (
										<PlanRow
											item={item}
											live={live()[item.name]}
											onDiff={(name) => props.post({ command: "diff", name })}
										/>
									)}
								</For>
							</Show>
						</ul>

						<div class={styles.footer}>{footer(r())}</div>
					</>
				)}
			</Show>
		</div>
	);

	function footer(r: PlanReport) {
		return (
			<Switch>
				<Match when={phase() === "applying"}>
					<div class={styles.footerMessage} role="status" ref={focusOnMount}>
						<span class={styles.hint}>Deploying…</span>
					</div>
				</Match>
				<Match when={phase() === "done"}>
					<div class={styles.footerMessage} role="status" ref={focusOnMount}>
						<div>
							<SegmentedCounts segments={doneSegs()} />
						</div>
						{closeRow()}
					</div>
				</Match>
				<Match when={phase() === "error"}>
					<div class={styles.footerMessage} ref={focusOnMount}>
						<span class={`${styles.doneHeadline} ${styles.fail}`} role="alert">
							{errorMsg()}
						</span>
						{closeRow()}
					</div>
				</Match>
				<Match when={phase() === "reviewing"}>
					<div class={styles.footerNote}>{note(r)}</div>
					<div class={styles.footerBar}>
						<div>
							<Show
								when={items().length > 0}
								fallback="No projections configured."
							>
								<SegmentedCounts segments={summarySegs()} />
							</Show>
						</div>
						<div class={styles.actions}>{actions(r)}</div>
					</div>
				</Match>
			</Switch>
		);
	}

	function note(r: PlanReport) {
		if (r.verdict === "blocked" && r.production) {
			return <Callout tone="error">{PROD_BLOCKED}</Callout>;
		}
		if (r.verdict === "blocked") {
			return (
				<Checkbox checked={bypass()} onChange={setBypass}>
					Deploy the valid projections, skip the rest
				</Checkbox>
			);
		}
		return null;
	}

	function actions(r: PlanReport) {
		if (items().length === 0) {
			return (
				<Button variant="secondary" onClick={cancel}>
					Close
				</Button>
			);
		}
		if (r.verdict === "in-sync") {
			return (
				<Button variant="secondary" onClick={cancel}>
					Close
				</Button>
			);
		}
		if (r.verdict === "blocked" && r.production) {
			return (
				<>
					<Button variant="secondary" onClick={cancel}>
						Cancel
					</Button>
					<Button variant="primary" disabled title={PROD_BLOCKED}>
						Deploy
					</Button>
				</>
			);
		}
		if (r.verdict === "blocked") {
			return (
				<>
					<Button variant="secondary" onClick={cancel}>
						Cancel
					</Button>
					<Button
						variant="primary"
						disabled={!bypass() || submitted()}
						onClick={() => deploy(true)}
					>
						Deploy
					</Button>
				</>
			);
		}
		return (
			<>
				<Button variant="secondary" onClick={cancel}>
					Cancel
				</Button>
				<Button
					variant="primary"
					disabled={submitted()}
					onClick={() => deploy(false)}
				>
					Deploy
				</Button>
			</>
		);
	}

	function closeRow() {
		return (
			<div class={styles.footerBar}>
				<span class={styles.hint}>You can close this window.</span>
				<Button variant="secondary" onClick={cancel}>
					Close
				</Button>
			</div>
		);
	}
}
