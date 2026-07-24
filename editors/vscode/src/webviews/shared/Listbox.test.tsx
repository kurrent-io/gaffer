import { render, fireEvent } from "@solidjs/testing-library";
import { createSignal } from "solid-js";
import { describe, expect, it, vi } from "vitest";
import { Listbox } from "./Listbox";

interface Row {
	id: number;
	label: string;
}
const rows: Row[] = [
	{ id: 1, label: "one" },
	{ id: 2, label: "two" },
	{ id: 3, label: "three" },
];

function req<T>(v: T | undefined): T {
	if (v === undefined) throw new Error("expected an element");
	return v;
}

function setup() {
	const onSelect = vi.fn();
	const [sel, setSel] = createSignal(2);
	const result = render(() => (
		<Listbox
			items={rows}
			getKey={(r) => r.id}
			isSelected={(r) => r.id === sel()}
			onSelect={(r) => {
				onSelect(r);
				setSel(r.id);
			}}
			renderItem={(r) => <span>{r.label}</span>}
			ariaLabel="rows"
		/>
	));
	return { ...result, onSelect };
}

describe("Listbox", () => {
	it("marks the selected option and makes it the only tabbable one", () => {
		const opts = setup().getAllByRole("option");
		expect(opts[1]?.getAttribute("aria-selected")).toBe("true");
		expect(opts[1]?.getAttribute("tabindex")).toBe("0");
		expect(opts[0]?.getAttribute("tabindex")).toBe("-1");
	});

	it("selects on click", () => {
		const { getByText, onSelect } = setup();
		fireEvent.click(getByText("three"));
		expect(onSelect).toHaveBeenCalledWith(rows[2]);
	});

	it("selects the focused option on Enter", () => {
		const { getAllByRole, onSelect } = setup();
		fireEvent.keyDown(req(getAllByRole("option")[0]), { key: "Enter" });
		expect(onSelect).toHaveBeenCalledWith(rows[0]);
	});

	it("ArrowDown roves focus without selecting", () => {
		const { getAllByRole, onSelect } = setup();
		fireEvent.keyDown(req(getAllByRole("option")[1]), { key: "ArrowDown" });
		expect(onSelect).not.toHaveBeenCalled();
		const opts = getAllByRole("option");
		expect(opts[2]?.getAttribute("tabindex")).toBe("0");
		expect(opts[1]?.getAttribute("tabindex")).toBe("-1");
	});
});
