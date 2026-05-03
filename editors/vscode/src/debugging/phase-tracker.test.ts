import { describe, expect, it } from "vitest";
import { PhaseTracker } from "./phase-tracker.js";

function track(): { tracker: PhaseTracker; labels: string[] } {
	const labels: string[] = [];
	const tracker = new PhaseTracker((l) => labels.push(l));
	return { tracker, labels };
}

describe("PhaseTracker", () => {
	it("starts in Connecting; first signal flips to Catching up", () => {
		const { tracker, labels } = track();
		expect(labels).toEqual(["Connecting"]);
		tracker.noteSignal();
		expect(labels.at(-1)).toBe("Catching up");
	});

	it("setCaughtUp(true) flips to Caught up; (false) flips back to Catching up", () => {
		const { tracker, labels } = track();
		tracker.setCaughtUp(true);
		expect(labels.at(-1)).toBe("Caught up");
		tracker.setCaughtUp(false);
		expect(labels.at(-1)).toBe("Catching up");
	});

	it("markEnded transitions to Disconnected", () => {
		const { tracker, labels } = track();
		tracker.setCaughtUp(true);
		tracker.markEnded();
		expect(labels.at(-1)).toBe("Disconnected");
	});

	it("reset returns to Connecting", () => {
		const { tracker, labels } = track();
		tracker.setCaughtUp(true);
		tracker.markEnded();
		tracker.reset();
		expect(labels.at(-1)).toBe("Connecting");
	});

	it("once ended, signals are silently dropped until reset", () => {
		// DAP teardown ordering is racy; a stray late event arriving
		// after markEnded() must not resurrect the tracker into a
		// running state.
		const { tracker, labels } = track();
		tracker.setCaughtUp(true);
		tracker.markEnded();
		const snapshot = labels.length;
		tracker.noteSignal();
		tracker.setCaughtUp(true);
		tracker.setCaughtUp(false);
		expect(labels.length).toBe(snapshot);
		expect(labels.at(-1)).toBe("Disconnected");
	});

	it("noteSignal is a no-op when already past Connecting", () => {
		const { tracker, labels } = track();
		tracker.noteSignal();
		const before = labels.length;
		tracker.noteSignal();
		expect(labels.length).toBe(before);
	});

	it("setCaughtUp is a no-op when both signal and value are unchanged", () => {
		const { tracker, labels } = track();
		tracker.setCaughtUp(true);
		const before = labels.length;
		tracker.setCaughtUp(true);
		expect(labels.length).toBe(before);
	});
});
