import { describe, expect, it } from "vitest";
import type { ProjectionInfo as RawProjectionInfo } from "@kurrent/gaffer-runtime";
import { mapProjectionInfo, streamMatchesSource } from "./ProjectionInfo.js";

const defaults: RawProjectionInfo = {
	allStreams: false,
	allEvents: false,
	categories: null,
	streams: null,
	events: null,
	byStreams: false,
	byCustomPartitions: false,
	biState: false,
	definesHandlers: false,
	definesStateTransform: false,
	producesResults: false,
	emitsEvents: false,
	handlesDeletedNotifications: false,
	includeLinks: false,
	resultStreamName: null,
	partitionResultStreamNamePattern: null,
	reorderEvents: false,
	processingLag: null,
	diagnostics: null,
};

describe("mapProjectionInfo", () => {
	it("maps fromAll", () => {
		const info = mapProjectionInfo({ ...defaults, allStreams: true });
		expect(info.source).toEqual({ type: "all" });
	});

	it("maps fromCategory", () => {
		const info = mapProjectionInfo({
			...defaults,
			categories: ["orders", "invoices"],
		});
		expect(info.source).toEqual({
			type: "categories",
			categories: ["orders", "invoices"],
		});
	});

	it("maps fromStreams", () => {
		const info = mapProjectionInfo({
			...defaults,
			streams: ["stream-1", "stream-2"],
		});
		expect(info.source).toEqual({
			type: "streams",
			streams: ["stream-1", "stream-2"],
		});
	});

	it("defaults to all when no source matches", () => {
		const info = mapProjectionInfo(defaults);
		expect(info.source).toEqual({ type: "all" });
	});

	it("treats empty categories array as all", () => {
		const info = mapProjectionInfo({ ...defaults, categories: [] });
		expect(info.source).toEqual({ type: "all" });
	});

	it("treats empty streams array as all", () => {
		const info = mapProjectionInfo({ ...defaults, streams: [] });
		expect(info.source).toEqual({ type: "all" });
	});

	it("maps foreachStream partitioning", () => {
		const info = mapProjectionInfo({ ...defaults, byStreams: true });
		expect(info.partitioning).toEqual({ type: "byStream" });
	});

	it("maps custom partitioning", () => {
		const info = mapProjectionInfo({ ...defaults, byCustomPartitions: true });
		expect(info.partitioning).toEqual({ type: "byCustomKey" });
	});

	it("maps no partitioning", () => {
		const info = mapProjectionInfo(defaults);
		expect(info.partitioning).toEqual({ type: "none" });
	});

	it("maps specific events", () => {
		const info = mapProjectionInfo({
			...defaults,
			events: ["OrderPlaced", "OrderShipped"],
		});
		expect(info.events).toEqual(["OrderPlaced", "OrderShipped"]);
	});

	it("maps all events", () => {
		const info = mapProjectionInfo({ ...defaults, allEvents: true });
		expect(info.events).toBe("all");
	});

	it("maps biState", () => {
		const info = mapProjectionInfo({ ...defaults, biState: true });
		expect(info.biState).toBe(true);
	});

	it("maps settings", () => {
		const info = mapProjectionInfo({
			...defaults,
			emitsEvents: true,
			includeLinks: true,
			reorderEvents: true,
			processingLag: 500,
			resultStreamName: "results",
			partitionResultStreamNamePattern: "result-{0}",
			handlesDeletedNotifications: true,
		});
		expect(info.settings).toEqual({
			emitsEvents: true,
			includeLinks: true,
			reorderEvents: true,
			processingLag: 500,
			resultStreamName: "results",
			partitionResultStreamNamePattern: "result-{0}",
			handlesDeletedNotifications: true,
		});
	});
});

describe("streamMatchesSource", () => {
	it("fromAll matches every stream", () => {
		const info = mapProjectionInfo({ ...defaults, allStreams: true });
		expect(streamMatchesSource(info, "anything-1")).toBe(true);
		expect(streamMatchesSource(info, "order-99")).toBe(true);
	});

	it("fromStream matches only the exact stream", () => {
		const info = mapProjectionInfo({ ...defaults, streams: ["s-1"] });
		expect(streamMatchesSource(info, "s-1")).toBe(true);
		expect(streamMatchesSource(info, "s-2")).toBe(false);
		expect(streamMatchesSource(info, "s-1-extra")).toBe(false);
	});

	it("fromStreams matches any listed stream", () => {
		const info = mapProjectionInfo({ ...defaults, streams: ["a", "b"] });
		expect(streamMatchesSource(info, "a")).toBe(true);
		expect(streamMatchesSource(info, "b")).toBe(true);
		expect(streamMatchesSource(info, "c")).toBe(false);
	});

	it("fromCategory matches the category prefix", () => {
		const info = mapProjectionInfo({ ...defaults, categories: ["cart"] });
		expect(streamMatchesSource(info, "cart-1")).toBe(true);
		expect(streamMatchesSource(info, "cart-abc")).toBe(true);
		expect(streamMatchesSource(info, "order-1")).toBe(false);
		expect(streamMatchesSource(info, "cart")).toBe(false);
	});

	it("matches $ce- category entries in the streams array by prefix", () => {
		const info = mapProjectionInfo({ ...defaults, streams: ["$ce-cart"] });
		expect(streamMatchesSource(info, "cart-1")).toBe(true);
		expect(streamMatchesSource(info, "order-1")).toBe(false);
	});

	it("treats a mixed $ce-/plain streams array as exact match", () => {
		// Mirrors buildSubscriptionFilter: not all-$ce- falls back to exact
		// name match, so $ce-order is literal, not a category prefix.
		const info = mapProjectionInfo({
			...defaults,
			streams: ["$ce-order", "my-stream"],
		});
		expect(streamMatchesSource(info, "$ce-order")).toBe(true);
		expect(streamMatchesSource(info, "my-stream")).toBe(true);
		expect(streamMatchesSource(info, "order-1")).toBe(false);
	});
});
