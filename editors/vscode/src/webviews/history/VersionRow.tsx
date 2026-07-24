import { Show } from "solid-js";
import { NoteLine } from "../shared/NoteLine";
import { ToneText } from "../shared/ToneText";
import {
	fmtTime,
	provenance,
	rollable,
	runState,
	shortHash,
	verbLabel,
	verbTone,
} from "./model";
import type { HistoryEntry } from "./protocol";
import styles from "./VersionRow.module.css";

// One timeline row: the verb, content hash (content versions only), and time,
// with a dimmed provenance line beneath. The run state reaches assistive tech
// only through the rail's colour, so carry it as a visually-hidden word. The
// `.main` line is tagged data-node so the rail can align its node to it.
export function VersionRow(props: { entry: HistoryEntry }) {
	const e = () => props.entry;
	const prov = () => provenance(e());
	return (
		<div class={styles.body}>
			<span class={styles.srOnly}>{runState(e())}</span>
			<div class={styles.main} data-node={e().version}>
				<ToneText tone={verbTone(e())} class={styles.op}>
					{verbLabel(e())}
				</ToneText>
				<Show when={rollable(e())}>
					<span class={styles.hash}>{shortHash(e().contentHash)}</span>
				</Show>
				<span class={styles.time}>{fmtTime(e().time)}</span>
			</div>
			<Show when={prov().text}>
				<NoteLine tone={prov().warn ? "warn" : "muted"} class={styles.prov}>
					{prov().text}
				</NoteLine>
			</Show>
		</div>
	);
}
