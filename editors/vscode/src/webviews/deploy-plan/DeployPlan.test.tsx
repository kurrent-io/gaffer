import { render } from "@solidjs/testing-library";
import { createSignal } from "solid-js";
import { describe, expect, it } from "vitest";
import { DeployPlan } from "./DeployPlan";
import type { DeployInbound } from "./protocol";

// A full plan-through-apply sequence delivered as one batch, to prove the
// inbox drains every message in order rather than coalescing to the last.
const batch: DeployInbound[] = [
	{
		type: "plan",
		token: 1,
		report: {
			verdict: "deployable",
			changes: 1,
			env: "staging",
			plan: [{ name: "orders", outcome: "created" }],
		},
	},
	{ type: "deploy-started" },
	{ type: "deploy-active", name: "orders" },
	{ type: "deploy-item", name: "orders", outcome: "created" },
	{
		type: "deploy-done",
		summary: {
			created: 1,
			updated: 0,
			rebuilt: 0,
			skipped: 0,
			refused: 0,
			invalid: 0,
			failed: 0,
		},
	},
];

describe("DeployPlan streaming", () => {
	it("applies every queued message in order when a batch drains at once", () => {
		const [inbox, setInbox] = createSignal<DeployInbound[]>([]);
		const { getByText, queryByText } = render(() => (
			<DeployPlan
				inbox={inbox()}
				onDrained={() => setInbox([])}
				post={() => {}}
			/>
		));

		setInbox(batch);

		// Ended at the done headline (not stuck at "Deploying…"), and the row's
		// mid-stream outcome from deploy-item ("created") was applied - so the
		// intermediate deltas weren't coalesced away.
		expect(getByText("Successfully deployed")).toBeTruthy();
		expect(queryByText("Deploying…")).toBeNull();
		expect(getByText("created")).toBeTruthy();
	});
});
