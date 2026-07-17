import { describe, expect, it } from "vitest";
import {
	DiffAuthRequiredError,
	DiffUnavailableError,
	diffRequestError,
	LSP_AUTH_REQUIRED,
	parseDiffResponse,
} from "./diff.js";

describe("diffRequestError", () => {
	it("maps the server's auth code to DiffAuthRequiredError", () => {
		const err = diffRequestError({ code: LSP_AUTH_REQUIRED, message: "nope" });
		expect(err).toBeInstanceOf(DiffAuthRequiredError);
	});

	it("maps any other rejection to DiffUnavailableError with the message", () => {
		const rpc = diffRequestError({ code: -32603, message: "boom" });
		expect(rpc).toBeInstanceOf(DiffUnavailableError);

		const plain = diffRequestError(new Error("connection refused"));
		expect(plain).toBeInstanceOf(DiffUnavailableError);
		expect(plain.message).toBe("connection refused");

		const str = diffRequestError("weird");
		expect(str).toBeInstanceOf(DiffUnavailableError);
		expect(str.message).toBe("weird");
	});
});

describe("parseDiffResponse", () => {
	it("accepts a well-formed payload and passes unmodelled fields through", () => {
		const diff = parseDiffResponse({
			name: "checkout",
			left: { ref: "deployed", hash: "aaa", source: "OLD" },
			right: { ref: "local", source: "NEW" },
			verdict: { drift: "drifted" },
			lines: [{ kind: "equal", text: "x" }],
		});
		expect(diff.name).toBe("checkout");
		expect(diff.left.source).toBe("OLD");
		expect(diff.right.source).toBe("NEW");
		expect(diff.verdict?.drift).toBe("drifted");
	});

	it("defaults a missing source to empty (the not-deployed signal)", () => {
		const diff = parseDiffResponse({
			name: "checkout",
			left: { ref: "deployed" },
			right: { ref: "local", source: "NEW" },
		});
		expect(diff.left.source).toBe("");
	});

	it("throws DiffUnavailableError on a malformed payload", () => {
		expect(() => parseDiffResponse({ name: "checkout" })).toThrow(
			DiffUnavailableError,
		);
		expect(() => parseDiffResponse("not an object")).toThrow(
			DiffUnavailableError,
		);
	});
});
