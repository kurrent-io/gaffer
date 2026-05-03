import { describe, expect, it } from "vitest";
import { type Phase, PhaseTracker } from "./phase-tracker.js";

function track(): { tracker: PhaseTracker; phases: Phase[] } {
	const phases: Phase[] = [];
	const tracker = new PhaseTracker((p) => phases.push(p));
	return { tracker, phases };
}

describe("PhaseTracker", () => {
	it("starts in connecting; first signal flips to catching-up", () => {
		const { tracker, phases } = track();
		expect(phases).toEqual(["connecting"]);
		tracker.noteSignal();
		expect(phases.at(-1)).toBe("catching-up");
	});

	it("setCaughtUp(true) flips to caught-up; (false) flips back to catching-up", () => {
		const { tracker, phases } = track();
		tracker.setCaughtUp(true);
		expect(phases.at(-1)).toBe("caught-up");
		tracker.setCaughtUp(false);
		expect(phases.at(-1)).toBe("catching-up");
	});

	it("markEnded transitions to disconnected", () => {
		const { tracker, phases } = track();
		tracker.setCaughtUp(true);
		tracker.markEnded();
		expect(phases.at(-1)).toBe("disconnected");
	});

	it("reset returns to connecting", () => {
		const { tracker, phases } = track();
		tracker.setCaughtUp(true);
		tracker.markEnded();
		tracker.reset();
		expect(phases.at(-1)).toBe("connecting");
	});

	it("once ended, signals are silently dropped until reset", () => {
		// DAP teardown ordering is racy; a stray late event arriving
		// after markEnded() must not resurrect the tracker into a
		// running state.
		const { tracker, phases } = track();
		tracker.setCaughtUp(true);
		tracker.markEnded();
		const snapshot = phases.length;
		tracker.noteSignal();
		tracker.setCaughtUp(true);
		tracker.setCaughtUp(false);
		expect(phases.length).toBe(snapshot);
		expect(phases.at(-1)).toBe("disconnected");
	});

	it("noteSignal is a no-op when already past connecting", () => {
		const { tracker, phases } = track();
		tracker.noteSignal();
		const before = phases.length;
		tracker.noteSignal();
		expect(phases.length).toBe(before);
	});

	it("setCaughtUp is a no-op when both signal and value are unchanged", () => {
		const { tracker, phases } = track();
		tracker.setCaughtUp(true);
		const before = phases.length;
		tracker.setCaughtUp(true);
		expect(phases.length).toBe(before);
	});
});
