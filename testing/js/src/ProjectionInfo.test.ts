import { describe, expect, it } from "vitest";
import type { QuerySources } from "@kurrent/gaffer-runtime";
import { mapQuerySources } from "./ProjectionInfo.js";

const defaults: QuerySources = {
	AllStreams: false,
	AllEvents: false,
	Categories: null,
	Streams: null,
	Events: null,
	ByStreams: false,
	ByCustomPartitions: false,
	IsBiState: false,
	DefinesFold: false,
	DefinesStateTransform: false,
	ProducesResults: false,
	HandlesDeletedNotifications: false,
	IncludeLinks: false,
	ResultStreamName: null,
	PartitionResultStreamNamePattern: null,
	ReorderEvents: false,
	ProcessingLag: null,
};

describe("mapQuerySources", () => {
	it("maps fromAll", () => {
		const info = mapQuerySources({ ...defaults, AllStreams: true });
		expect(info.source).toEqual({ type: "all" });
	});

	it("maps fromCategory", () => {
		const info = mapQuerySources({
			...defaults,
			Categories: ["orders", "invoices"],
		});
		expect(info.source).toEqual({
			type: "categories",
			categories: ["orders", "invoices"],
		});
	});

	it("maps fromStreams", () => {
		const info = mapQuerySources({
			...defaults,
			Streams: ["stream-1", "stream-2"],
		});
		expect(info.source).toEqual({
			type: "streams",
			streams: ["stream-1", "stream-2"],
		});
	});

	it("defaults to all when no source matches", () => {
		const info = mapQuerySources(defaults);
		expect(info.source).toEqual({ type: "all" });
	});

	it("treats empty Categories array as all", () => {
		const info = mapQuerySources({ ...defaults, Categories: [] });
		expect(info.source).toEqual({ type: "all" });
	});

	it("treats empty Streams array as all", () => {
		const info = mapQuerySources({ ...defaults, Streams: [] });
		expect(info.source).toEqual({ type: "all" });
	});

	it("maps foreachStream partitioning", () => {
		const info = mapQuerySources({ ...defaults, ByStreams: true });
		expect(info.partitioning).toEqual({ type: "byStream" });
	});

	it("maps custom partitioning", () => {
		const info = mapQuerySources({ ...defaults, ByCustomPartitions: true });
		expect(info.partitioning).toEqual({ type: "byCustomKey" });
	});

	it("maps no partitioning", () => {
		const info = mapQuerySources(defaults);
		expect(info.partitioning).toEqual({ type: "none" });
	});

	it("maps specific events", () => {
		const info = mapQuerySources({
			...defaults,
			Events: ["OrderPlaced", "OrderShipped"],
		});
		expect(info.events).toEqual(["OrderPlaced", "OrderShipped"]);
	});

	it("maps all events", () => {
		const info = mapQuerySources({ ...defaults, AllEvents: true });
		expect(info.events).toBe("all");
	});

	it("maps biState", () => {
		const info = mapQuerySources({ ...defaults, IsBiState: true });
		expect(info.biState).toBe(true);
	});

	it("maps settings", () => {
		const info = mapQuerySources({
			...defaults,
			IncludeLinks: true,
			ReorderEvents: true,
			ProcessingLag: 500,
			ResultStreamName: "results",
			PartitionResultStreamNamePattern: "result-{0}",
			HandlesDeletedNotifications: true,
		});
		expect(info.settings).toEqual({
			includeLinks: true,
			reorderEvents: true,
			processingLag: 500,
			resultStreamName: "results",
			partitionResultStreamNamePattern: "result-{0}",
			handlesDeletedNotifications: true,
		});
	});
});
