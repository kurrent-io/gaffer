import { describe, expect, it } from "vitest";
import { systemEvents } from "./systemEvents.js";

describe("systemEvents", () => {
	it("creates streamDeleted event", () => {
		const event = systemEvents.streamDeleted("cart-123", 0);
		expect(event.eventType).toBe("$streamDeleted");
		expect(event.streamId).toBe("cart-123");
		expect(event.sequenceNumber).toBe(0);
		expect(event.data).toEqual({ streamId: "cart-123" });
	});
});
