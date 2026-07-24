import { render, fireEvent } from "@solidjs/testing-library";
import { describe, expect, it, vi } from "vitest";
import { Button } from "./Button";

describe("Button", () => {
	it("renders its label and fires onClick", () => {
		const onClick = vi.fn();
		const { getByRole } = render(() => (
			<Button variant="primary" onClick={onClick}>
				Deploy
			</Button>
		));
		const btn = getByRole("button", { name: "Deploy" });
		fireEvent.click(btn);
		expect(onClick).toHaveBeenCalledOnce();
	});

	it("passes through disabled", () => {
		const { getByRole } = render(() => <Button disabled>Nope</Button>);
		expect((getByRole("button") as HTMLButtonElement).disabled).toBe(true);
	});
});
