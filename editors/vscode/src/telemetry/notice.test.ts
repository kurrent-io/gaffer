import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { load, type TelemetryConfig } from "./config.js";
import {
	type FirstRunChoice,
	type RunFirstRunNoticeArgs,
	runFirstRunNotice,
} from "./notice.js";

let dir: string;

beforeEach(() => {
	dir = fs.mkdtempSync(path.join(os.tmpdir(), "gaffer-telemetry-notice-"));
});
afterEach(() => {
	fs.rmSync(dir, { recursive: true, force: true });
});

function buildArgs(
	overrides: Partial<RunFirstRunNoticeArgs> = {},
): RunFirstRunNoticeArgs {
	return {
		storageDir: dir,
		config: {},
		optedOut: false,
		prompt: vi.fn(async () => "dismiss" as FirstRunChoice),
		openLearnMore: vi.fn(async () => {}),
		...overrides,
	};
}

describe("runFirstRunNotice", () => {
	it("skips entirely when disclosed=true is already latched", async () => {
		const prompt = vi.fn(async () => "dismiss" as FirstRunChoice);
		await runFirstRunNotice(buildArgs({ config: { disclosed: true }, prompt }));
		expect(prompt).not.toHaveBeenCalled();
		// No file written - load returns empty.
		expect(await load(dir)).toEqual({});
	});

	it("skips entirely when an opt-out signal is already active", async () => {
		const prompt = vi.fn(async () => "dismiss" as FirstRunChoice);
		await runFirstRunNotice(buildArgs({ optedOut: true, prompt }));
		expect(prompt).not.toHaveBeenCalled();
		expect(await load(dir)).toEqual({});
	});

	it("on [Disable]: writes telemetry_enabled=false + disclosed=true", async () => {
		await runFirstRunNotice(buildArgs({ prompt: async () => "disable" }));
		expect(await load(dir)).toEqual({
			telemetry_enabled: false,
			disclosed: true,
		});
	});

	it("on [Dismiss]: writes telemetry_enabled=true + disclosed=true", async () => {
		await runFirstRunNotice(buildArgs({ prompt: async () => "dismiss" }));
		expect(await load(dir)).toEqual({
			telemetry_enabled: true,
			disclosed: true,
		});
	});

	it("on undefined (X-close): treats as dismiss, latches as accept", async () => {
		await runFirstRunNotice(buildArgs({ prompt: async () => undefined }));
		expect(await load(dir)).toEqual({
			telemetry_enabled: true,
			disclosed: true,
		});
	});

	it("on [Learn more]: opens URL, leaves disclosed unset (not a decision)", async () => {
		const openLearnMore = vi.fn(async () => {});
		await runFirstRunNotice(
			buildArgs({ prompt: async () => "learn-more", openLearnMore }),
		);
		expect(openLearnMore).toHaveBeenCalledTimes(1);
		// No config write - next activation re-shows the notice.
		expect(await load(dir)).toEqual({});
	});

	it("preserves existing identity fields when latching disclosed", async () => {
		const seed: TelemetryConfig = {
			telemetry_id: "8f2b1a4c-9e7d-4a3e-b5f2-7c8a9d4e1f02",
			salt: "11111111-2222-3333-4444-555555555555",
		};
		await runFirstRunNotice(
			buildArgs({ config: seed, prompt: async () => "dismiss" }),
		);
		expect(await load(dir)).toEqual({
			...seed,
			telemetry_enabled: true,
			disclosed: true,
		});
	});
});
