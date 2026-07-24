import { createEffect, createMemo, createSignal, on, Show } from "solid-js";
import { Listbox } from "../shared/Listbox";
import { Notice } from "../shared/Notice";
import { DetailPanel } from "./DetailPanel";
import { verbLabel } from "./model";
import { Rail, railWidth } from "./Rail";
import { VersionRow } from "./VersionRow";
import type {
	HistoryEntry,
	HistoryGraph,
	HistoryInbound,
	HistoryOutbound,
} from "./protocol";
import styles from "./History.module.css";

const EMPTY_GRAPH: HistoryGraph = { nodeLane: [], spans: [] };

export function History(props: {
	inbox: HistoryInbound[];
	onDrained: () => void;
	post: (message: HistoryOutbound) => void;
}) {
	const [name, setName] = createSignal("");
	const [env, setEnv] = createSignal("");
	const [entries, setEntries] = createSignal<HistoryEntry[]>([]);
	const [graph, setGraph] = createSignal<HistoryGraph>(EMPTY_GRAPH);
	const [token, setToken] = createSignal(0);
	const [selected, setSelected] = createSignal<number | null>(null);
	const [rolling, setRolling] = createSignal<number | null>(null);
	const [error, setError] = createSignal<string | null>(null);
	// False until the first message lands, so a pre-message render shows nothing
	// rather than a "No deploy history for '' on ." flash.
	const [received, setReceived] = createSignal(false);

	// Reuse the object for a version we've already rendered so <For> keeps its
	// DOM node (and the rail its measured node) across a refresh - a version's
	// ledger data is immutable, so identity-by-version is safe. Without this the
	// host's post-rollback re-post would rebuild every row and drop keyboard focus.
	const rows = createMemo<HistoryEntry[]>((prev) => {
		const byVersion = new Map(prev.map((e) => [e.version, e]));
		return entries().map((e) => byVersion.get(e.version) ?? e);
	}, []);

	let timeline: HTMLDivElement | undefined;

	// Drain the inbox in order, applying every queued message, then clear it - so
	// no streaming delta is dropped even if several land before this runs. on()
	// tracks only props.inbox (not the signals handle() writes), so handle's own
	// setState can't retrigger this; clearing can't lose a message because
	// handle() is synchronous, so nothing arrives mid-drain.
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

	function handle(msg: HistoryInbound) {
		setReceived(true);
		switch (msg.type) {
			case "history": {
				// Version numbers are per-projection and overlap; drop the selection
				// on a switch, keep it on a same-projection refresh (e.g. a rollback).
				const switched = msg.name !== name() || msg.env !== env();
				setName(msg.name);
				setEnv(msg.env);
				setGraph(msg.graph);
				setToken(msg.token);
				setEntries(msg.entries);
				setError(null);
				setRolling(null);
				reconcileSelection(msg.entries, switched);
				break;
			}
			case "error":
				// Clear the timeline so a stale one doesn't linger with live actions.
				setEntries([]);
				setGraph(EMPTY_GRAPH);
				setSelected(null);
				setRolling(null);
				setError(msg.message);
				break;
			case "rollback-active":
				setRolling(msg.version);
				break;
			case "rollback-done":
			case "rollback-error":
				// On success the host also re-posts a fresh "history"; clearing here
				// first means a reload that fails can't leave the detail stuck.
				setRolling(null);
				break;
		}
	}

	function reconcileSelection(es: HistoryEntry[], switched: boolean) {
		const first = es[0];
		if (!first) {
			setSelected(null);
			return;
		}
		const cur = switched ? null : selected();
		setSelected(
			cur !== null && es.some((e) => e.version === cur) ? cur : first.version,
		);
	}

	const selectedEntry = () => rows().find((e) => e.version === selected());
	const verbW = () =>
		`${rows().reduce((m, e) => Math.max(m, verbLabel(e).length), 0)}ch`;
	const count = () =>
		`${rows().length} ${rows().length === 1 ? "version" : "versions"}`;

	return (
		<div class={styles.root}>
			<div class={styles.header}>
				<span class={styles.name}>{name()}</span>
				<span class={styles.env}>· {env()}</span>
				<span class={styles.count}>{count()}</span>
			</div>
			<div class={styles.timelinePane}>
				<Show when={!error() && rows().length > 0}>
					<div class={styles.timeline} ref={timeline}>
						<Rail
							entries={rows()}
							graph={graph()}
							measureRoot={() => timeline}
						/>
						<div
							class={styles.rows}
							style={{
								"margin-left": `${railWidth(graph())}px`,
								"--verb-w": verbW(),
							}}
						>
							<Listbox
								items={rows()}
								getKey={(e) => e.version}
								isSelected={(e) => e.version === selected()}
								onSelect={(e) => setSelected(e.version)}
								renderItem={(e) => <VersionRow entry={e} />}
								ariaLabel="Deploy versions"
							/>
						</div>
					</div>
				</Show>
				<Show when={received() && !error() && rows().length === 0}>
					<Notice>
						No deploy history for "{name()}" on {env()}.
					</Notice>
				</Show>
				<Show when={error()}>
					{(message) => <Notice tone="error">{message()}</Notice>}
				</Show>
			</div>
			<DetailPanel
				entry={selectedEntry()}
				entries={rows()}
				graph={graph()}
				rollingVersion={rolling()}
				onRollback={(version) =>
					props.post({ command: "rollback", version, token: token() })
				}
				onDiff={(version, framing) =>
					props.post({ command: "diff", version, framing })
				}
			/>
		</div>
	);
}
