// Tracks the projection's connection / catch-up phase across the
// lifetime of a debug session and pushes the label to a single
// consumer (the Status view's description chip).
//
// Phase progression:
//   connecting -> catching-up <-> caught-up -> disconnected
//
// connecting is the truthful starting position - we haven't heard
// from the CLI yet. The first DAP signal (any gaffer/* event) flips
// us out of it. caughtUp drives the catching-up <-> caught-up
// transitions thereafter. markEnded() terminates with disconnected;
// once ended the state is frozen until reset() (signals arriving
// during teardown can't resurrect a dead session).

export type Phase = "connecting" | "catching-up" | "caught-up" | "disconnected";

export const PHASE_LABELS: Record<Phase, string> = {
	connecting: "Connecting",
	"catching-up": "Catching up",
	"caught-up": "Caught up",
	disconnected: "Disconnected",
};

export type PhaseSetter = (phase: Phase) => void;

export class PhaseTracker {
	readonly #setter: PhaseSetter;
	#hasSignal = false;
	#caughtUp = false;
	#ended = false;

	constructor(setter: PhaseSetter) {
		this.#setter = setter;
		this.#apply();
	}

	reset(): void {
		this.#hasSignal = false;
		this.#caughtUp = false;
		this.#ended = false;
		this.#apply();
	}

	// Any DAP signal proves the CLI is talking to us; promotes us out
	// of "connecting". Once ended, signals are silently dropped - DAP
	// teardown is racy and we don't want a stray late event to
	// resurrect a dead session.
	noteSignal(): void {
		if (this.#ended || this.#hasSignal) return;
		this.#hasSignal = true;
		this.#apply();
	}

	setCaughtUp(caughtUp: boolean): void {
		if (this.#ended) return;
		if (this.#hasSignal && this.#caughtUp === caughtUp) return;
		this.#hasSignal = true;
		this.#caughtUp = caughtUp;
		this.#apply();
	}

	markEnded(): void {
		if (this.#ended) return;
		this.#ended = true;
		this.#apply();
	}

	get phase(): Phase {
		if (this.#ended) return "disconnected";
		if (!this.#hasSignal) return "connecting";
		return this.#caughtUp ? "caught-up" : "catching-up";
	}

	#apply(): void {
		this.#setter(this.phase);
	}
}
