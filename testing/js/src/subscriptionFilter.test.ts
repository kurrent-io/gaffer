import { describe, it, expect } from "vitest";
import { buildSubscriptionFilter, escapeRegex } from "./subscriptionFilter.js";
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
