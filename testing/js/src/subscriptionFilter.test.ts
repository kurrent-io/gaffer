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
		expect(getResolveLinks("v1")).toBe(false);
	});

	it("returns true for v2", () => {
		expect(getResolveLinks("v2")).toBe(true);
	});

	it("defaults to true (v2)", () => {
		expect(getResolveLinks()).toBe(true);
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
