import { describe, it, expect } from "vitest";
import {
	buildSubscriptionFilter,
	escapeRegex,
	getResolveLinks,
} from "./subscriptionFilter.js";
import type { ProjectionInfo } from "./ProjectionInfo.js";

function makeInfo(overrides: Partial<ProjectionInfo> = {}): ProjectionInfo {
	return {
		source: { type: "all" },
		events: "all",
		partitioning: { type: "none" },
		biState: false,
		settings: {
			emitsEvents: false,
			includeLinks: false,
			reorderEvents: false,
			processingLag: null,
			resultStreamName: null,
			partitionResultStreamNamePattern: null,
			handlesDeletedNotifications: false,
		},
		...overrides,
	};
}

describe("buildSubscriptionFilter", () => {
	it("returns undefined for fromAll with all events", () => {
		expect(buildSubscriptionFilter(makeInfo())).toBeUndefined();
	});

	it("returns event type filter for fromAll with specific events", () => {
		const filter = buildSubscriptionFilter(
			makeInfo({ events: ["OrderPlaced", "OrderShipped"] }),
		);
		expect(filter).toMatchObject({
			filterOn: "eventType",
			prefixes: ["OrderPlaced", "OrderShipped"],
		});
	});

	it("returns no filter when events is 'all' (e.g. $any handler) regardless of source", () => {
		// Cross-language regression: bindings that flatten the
		// `events: "all" | string[]` union into separate fields can
		// accidentally narrow on the array even when `all` is set,
		// silently dropping every event type a $any handler should
		// have caught. TS's tagged union prevents the same trip-up
		// here, but the behaviour is still spec-required.
		expect(
			buildSubscriptionFilter(makeInfo({ events: "all" })),
		).toBeUndefined();
	});

	it("sets maxSearchWindow and checkpointInterval on filtered subscriptions", () => {
		// Client defaults (32 / 1) make CaughtUp effectively never
		// fire on a busy store. Every filter we build should override
		// them - see specs/subscription.md.
		const cases: ProjectionInfo[] = [
			makeInfo({ events: ["OrderPlaced"] }),
			makeInfo({ source: { type: "streams", streams: ["orders"] } }),
			makeInfo({ source: { type: "streams", streams: ["$ce-order"] } }),
			makeInfo({ source: { type: "categories", categories: ["order"] } }),
		];
		for (const info of cases) {
			const filter = buildSubscriptionFilter(info);
			expect(filter).toMatchObject({
				maxSearchWindow: expect.any(Number),
				checkpointInterval: expect.any(Number),
			});
		}
	});

	it("returns stream name regex for fromStreams", () => {
		const filter = buildSubscriptionFilter(
			makeInfo({ source: { type: "streams", streams: ["orders", "carts"] } }),
		);
		expect(filter).toMatchObject({
			filterOn: "streamName",
			regex: "^(orders|carts)$",
		});
	});

	it("escapes special regex chars in stream names", () => {
		const filter = buildSubscriptionFilter(
			makeInfo({
				source: { type: "streams", streams: ["orders.v2", "carts (new)"] },
			}),
		);
		expect(filter).toMatchObject({
			filterOn: "streamName",
			regex: "^(orders\\.v2|carts \\(new\\))$",
		});
	});

	it("returns stream name prefix for fromCategory", () => {
		const filter = buildSubscriptionFilter(
			makeInfo({ source: { type: "categories", categories: ["order"] } }),
		);
		expect(filter).toMatchObject({
			filterOn: "streamName",
			prefixes: ["order-"],
		});
	});

	it("handles multiple categories", () => {
		const filter = buildSubscriptionFilter(
			makeInfo({
				source: { type: "categories", categories: ["order", "cart"] },
			}),
		);
		expect(filter).toMatchObject({
			filterOn: "streamName",
			prefixes: ["order-", "cart-"],
		});
	});

	it("adds $streamDeleted and $metadata prefixes when handlesDeletedNotifications", () => {
		const filter = buildSubscriptionFilter(
			makeInfo({
				events: ["OrderPlaced"],
				settings: {
					emitsEvents: false,
					includeLinks: false,
					reorderEvents: false,
					processingLag: null,
					resultStreamName: null,
					partitionResultStreamNamePattern: null,
					handlesDeletedNotifications: true,
				},
			}),
		);
		expect(filter).toMatchObject({
			filterOn: "eventType",
			prefixes: ["OrderPlaced", "$streamDeleted", "$metadata"],
		});
	});

	it("converts $ce- streams to category prefix filters", () => {
		const filter = buildSubscriptionFilter(
			makeInfo({
				source: {
					type: "streams",
					streams: ["$ce-order", "$ce-cart"],
				},
			}),
		);
		expect(filter).toMatchObject({
			filterOn: "streamName",
			prefixes: ["order-", "cart-"],
		});
	});

	it("treats mixed $ce- and regular streams as exact match", () => {
		const filter = buildSubscriptionFilter(
			makeInfo({
				source: {
					type: "streams",
					streams: ["$ce-order", "my-stream"],
				},
			}),
		);
		expect(filter).toMatchObject({
			filterOn: "streamName",
			regex: expect.stringContaining("\\$ce-order"),
		});
	});
});

describe("getResolveLinks", () => {
	it("returns false for v1", () => {
		expect(getResolveLinks(1)).toBe(false);
	});

	it("returns true for v2", () => {
		expect(getResolveLinks(2)).toBe(true);
	});
});

describe("escapeRegex", () => {
	it("escapes special characters", () => {
		expect(escapeRegex("hello.world")).toBe("hello\\.world");
		expect(escapeRegex("a+b*c?")).toBe("a\\+b\\*c\\?");
		expect(escapeRegex("foo[bar]")).toBe("foo\\[bar\\]");
		expect(escapeRegex("a(b)c")).toBe("a\\(b\\)c");
		expect(escapeRegex("$stream")).toBe("\\$stream");
	});

	it("leaves normal strings unchanged", () => {
		expect(escapeRegex("orders-123")).toBe("orders-123");
	});
});
